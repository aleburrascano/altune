package persistence

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PostgresUpgradeStore struct {
	pool *pgxpool.Pool
}

var (
	_ ports.UpgradeStore = (*PostgresUpgradeStore)(nil)
	_ ports.UpgradeState = (*PostgresUpgradeStore)(nil)
)

func NewPostgresUpgradeStore(pool *pgxpool.Pool) *PostgresUpgradeStore {
	return &PostgresUpgradeStore{pool: pool}
}

const upgradeCandidatesSQL = `
SELECT t.id, t.user_id, COALESCE(t.acquisition_evidence->>'grade', 'D')
FROM tracks t
LEFT JOIN acquisition_upgrade_state s ON s.track_id = t.id
LEFT JOIN acquisition_jobs j ON j.track_id = t.id
WHERE t.acquisition_status = 'ready'
	AND t.audio_ref IS NOT NULL
	AND (COALESCE(t.audio_source_url, '') <> '' OR ($5::boolean AND COALESCE((s.evidence->>'in_place_generation')::int, 0) < $2))
	AND j.available_at IS NULL
	AND COALESCE(t.acquisition_evidence->>'grade', 'D') NOT IN ('A_user', 'best_available')
	AND (
		COALESCE(t.acquisition_evidence->>'grade', 'D') <> 'A'
		OR ($5::boolean AND COALESCE((s.evidence->>'in_place_generation')::int, 0) < $2)
		OR ($6::int > 0 AND (
			t.acquisition_evidence->>'source_format_id' = '18'
			OR COALESCE(NULLIF(t.acquisition_evidence->>'source_kbps', '')::int, 0) BETWEEN 1 AND $6::int - 1))
	)
	AND COALESCE((s.evidence->>'best_available_generation')::int, 0) < $2
	AND COALESCE((s.evidence->>'upgrade_tried_at')::timestamptz, '-infinity') < now() - make_interval(secs => $3)
	AND (
		SELECT count(*) FROM acquisition_jobs q
		WHERE q.job_kind = 'upgrade' AND q.available_at IS NOT NULL
	) < $4
ORDER BY array_position($7::text[], COALESCE(t.acquisition_evidence->>'grade', 'D')) ASC NULLS FIRST,
	(s.evidence->>'upgrade_tried_at')::timestamptz ASC NULLS FIRST,
	t.id
LIMIT $1`

func worstFirstGrades() []string {
	grades := audioquality.WorstFirst()
	names := make([]string, len(grades))
	for i, g := range grades {
		names[i] = string(g)
	}
	return names
}

func (s *PostgresUpgradeStore) Candidates(ctx context.Context, selection ports.UpgradeSelection) ([]ports.UpgradeCandidate, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	rows, err := s.pool.Query(ctx, upgradeCandidatesSQL,
		selection.Limit, selection.Generation, selection.RetryAfter.Seconds(), selection.MaxQueued, selection.CheckInPlace,
		selection.LowBitrateFloorKbps, worstFirstGrades())
	if err != nil {
		return nil, fmt.Errorf("select upgrade candidates: %w", err)
	}
	defer rows.Close()
	candidates, err := pgx.CollectRows(rows, scanUpgradeCandidate)
	if err != nil {
		return nil, fmt.Errorf("read upgrade candidates: %w", err)
	}
	return candidates, nil
}

func scanUpgradeCandidate(row pgx.CollectableRow) (ports.UpgradeCandidate, error) {
	var trackID, userID uuid.UUID
	var grade string
	if err := row.Scan(&trackID, &userID, &grade); err != nil {
		return ports.UpgradeCandidate{}, err
	}
	return ports.UpgradeCandidate{TrackID: ids.TrackIdFromUUID(trackID), UserID: ids.NewUserId(userID), Grade: grade}, nil
}

const noteUpgradeAttemptSQL = `
INSERT INTO acquisition_upgrade_state AS s (track_id, evidence, rejected_source_keys)
SELECT t.id,
	jsonb_build_object('upgrade_tried_at', now(), 'upgrade_tries', $3::int),
	CASE WHEN $2::text = '' THEN '{}'::text[] ELSE ARRAY[$2::text] END
FROM tracks t WHERE t.id = $1 FOR NO KEY UPDATE OF t
ON CONFLICT (track_id) DO UPDATE SET
	evidence = s.evidence || jsonb_build_object(
		'upgrade_tried_at', now(),
		'upgrade_tries', COALESCE((s.evidence->>'upgrade_tries')::int, 0) + $3::int),
	rejected_source_keys = CASE
		WHEN $2::text = '' OR $2::text = ANY(s.rejected_source_keys) THEN s.rejected_source_keys
		ELSE (array_append(s.rejected_source_keys, $2::text))[
			greatest(cardinality(s.rejected_source_keys) + 2 - $4::int, 1):]
	END
RETURNING (evidence->>'upgrade_tries')::int`

func (s *PostgresUpgradeStore) NoteAttempt(ctx context.Context, trackID ids.TrackId, attempt ports.UpgradeAttempt) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	counted := 0
	if attempt.CountsAsTry {
		counted = 1
	}
	var tries int
	err := s.pool.QueryRow(ctx, noteUpgradeAttemptSQL,
		trackID.UUID(), attempt.RejectedSourceKey, counted, ports.MaxRejectedSourceKeys).Scan(&tries)
	if err != nil {
		return 0, fmt.Errorf("note upgrade attempt: %w", err)
	}
	return tries, nil
}

const giveUpUpgradeSQL = `
INSERT INTO acquisition_upgrade_state AS s (track_id, evidence)
SELECT t.id, jsonb_build_object(
		'best_available_generation', $2::int,
		'best_available_reason', $3::text,
		'best_available_at', now(),
		'upgrade_tries', 0)
FROM tracks t WHERE t.id = $1 FOR NO KEY UPDATE OF t
ON CONFLICT (track_id) DO UPDATE SET
	evidence = s.evidence || EXCLUDED.evidence`

func (s *PostgresUpgradeStore) GiveUp(ctx context.Context, trackID ids.TrackId, generation int, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	if _, err := s.pool.Exec(ctx, giveUpUpgradeSQL, trackID.UUID(), generation, reason); err != nil {
		return fmt.Errorf("give up upgrade: %w", err)
	}
	return nil
}

const recordInPlaceStateSQL = `
INSERT INTO acquisition_upgrade_state AS s (track_id, evidence)
SELECT t.id, jsonb_strip_nulls(jsonb_build_object(
		'in_place_generation', $2::int,
		'in_place_at', now(),
		'in_place_basis', NULLIF($3::text, ''),
		'in_place_bit_error_rate', NULLIF($4::float8, 0)))
FROM tracks t WHERE t.id = $1 FOR NO KEY UPDATE OF t
ON CONFLICT (track_id) DO UPDATE SET
	evidence = (s.evidence - 'in_place_failures') || EXCLUDED.evidence`

func (s *PostgresUpgradeStore) RecordInPlaceState(ctx context.Context, trackID ids.TrackId, generation int, basis string, bitErrorRate float64) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	if _, err := s.pool.Exec(ctx, recordInPlaceStateSQL, trackID.UUID(), generation, basis, bitErrorRate); err != nil {
		return fmt.Errorf("record in-place state: %w", err)
	}
	return nil
}

const noteInPlaceFailureSQL = `
INSERT INTO acquisition_upgrade_state AS s (track_id, evidence)
SELECT t.id, jsonb_build_object('upgrade_tried_at', now(), 'in_place_failures', 1)
FROM tracks t WHERE t.id = $1 FOR NO KEY UPDATE OF t
ON CONFLICT (track_id) DO UPDATE SET
	evidence = s.evidence || jsonb_build_object(
		'upgrade_tried_at', now(),
		'in_place_failures', COALESCE((s.evidence->>'in_place_failures')::int, 0) + 1)
RETURNING (evidence->>'in_place_failures')::int`

func (s *PostgresUpgradeStore) NoteInPlaceFailure(ctx context.Context, trackID ids.TrackId) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	var failures int
	if err := s.pool.QueryRow(ctx, noteInPlaceFailureSQL, trackID.UUID()).Scan(&failures); err != nil {
		return 0, fmt.Errorf("note in-place failure: %w", err)
	}
	return failures, nil
}

func (s *PostgresUpgradeStore) State(ctx context.Context, trackID ids.TrackId) (ports.UpgradeStateSnapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	var snapshot ports.UpgradeStateSnapshot
	var evidence []byte
	err := s.pool.QueryRow(ctx,
		`SELECT evidence, rejected_source_keys FROM acquisition_upgrade_state WHERE track_id = $1`,
		trackID.UUID()).Scan(&evidence, &snapshot.RejectedSourceKeys)
	if errors.Is(err, pgx.ErrNoRows) {
		return ports.UpgradeStateSnapshot{}, nil
	}
	if err != nil {
		return ports.UpgradeStateSnapshot{}, fmt.Errorf("read upgrade state: %w", err)
	}
	snapshot.Evidence = evidence
	return snapshot, nil
}

const lockTrackSQL = `SELECT 1 FROM tracks WHERE id = $1 FOR NO KEY UPDATE`

const readJobFenceSQL = `SELECT attempts FROM acquisition_jobs WHERE track_id = $1 FOR SHARE`

func checkJobFence(ctx context.Context, tx pgx.Tx, trackID ids.TrackId, fence ports.ClaimNumber) error {
	var current int
	err := tx.QueryRow(ctx, readJobFenceSQL, trackID.UUID()).Scan(&current)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("read job fence: %w", err)
	}
	if fence > 0 && current > 0 && current != int(fence) {
		return fmt.Errorf("track %s claimed by attempt %d, caller holds %d: %w", trackID, current, fence, acqdomain.ErrLeaseLost)
	}
	return nil
}

const ensureUpgradeStateSQL = `
INSERT INTO acquisition_upgrade_state (track_id) SELECT id FROM tracks WHERE id = $1 ON CONFLICT (track_id) DO NOTHING`

const lockUpgradeStateSQL = `SELECT rejected_source_keys FROM acquisition_upgrade_state WHERE track_id = $1 FOR UPDATE`

const settleUpgradeStateSQL = `
UPDATE acquisition_upgrade_state
SET rejected_source_keys = $2,
	evidence = CASE WHEN $3::boolean THEN '{}'::jsonb ELSE evidence END
WHERE track_id = $1`

func (s *PostgresUpgradeStore) SettleAcquisition(ctx context.Context, trackID ids.TrackId, fence ports.ClaimNumber, rejectedSourceKeys []string, resetEvidence bool) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	err := database.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, lockTrackSQL, trackID.UUID()); err != nil {
			return fmt.Errorf("lock track: %w", err)
		}
		if err := checkJobFence(ctx, tx, trackID, fence); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, ensureUpgradeStateSQL, trackID.UUID()); err != nil {
			return fmt.Errorf("ensure upgrade state: %w", err)
		}
		var existing []string
		if err := tx.QueryRow(ctx, lockUpgradeStateSQL, trackID.UUID()).Scan(&existing); err != nil {
			return fmt.Errorf("lock upgrade state: %w", err)
		}
		merged := ports.AppendRejectedSourceKeys(existing, rejectedSourceKeys)
		if _, err := tx.Exec(ctx, settleUpgradeStateSQL, trackID.UUID(), merged, resetEvidence); err != nil {
			return fmt.Errorf("settle upgrade state: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("settle acquisition state: %w", err)
	}
	return nil
}
