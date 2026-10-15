package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgxUpgradeStore struct {
	pool *pgxpool.Pool
}

var (
	_ ports.UpgradeStore = (*PgxUpgradeStore)(nil)
	_ ports.InPlaceStore = (*PgxUpgradeStore)(nil)
)

func NewPgxUpgradeStore(pool *pgxpool.Pool) *PgxUpgradeStore {
	return &PgxUpgradeStore{pool: pool}
}

const upgradeCandidatesSQL = `
SELECT t.id, t.user_id, COALESCE(t.acquisition_evidence->>'grade', 'D')
FROM tracks t
WHERE t.acquisition_status = 'ready'
	AND t.audio_ref IS NOT NULL
	AND (COALESCE(t.audio_source_url, '') <> '' OR ($5::boolean AND COALESCE((t.acquisition_evidence->>'in_place_generation')::int, 0) < $2))
	AND t.acquisition_available_at IS NULL
	AND COALESCE(t.acquisition_evidence->>'grade', 'D') NOT IN ('A', 'A_user')
	AND COALESCE((t.acquisition_evidence->>'best_available_generation')::int, 0) < $2
	AND COALESCE((t.acquisition_evidence->>'upgrade_tried_at')::timestamptz, '-infinity') < now() - make_interval(secs => $3)
	AND (
		SELECT count(*) FROM tracks q
		WHERE q.acquisition_job_kind = 'upgrade' AND q.acquisition_available_at IS NOT NULL
	) < $4
ORDER BY COALESCE(t.acquisition_evidence->>'grade', 'D') DESC,
	(t.acquisition_evidence->>'upgrade_tried_at')::timestamptz ASC NULLS FIRST,
	t.id
LIMIT $1`

func (s *PgxUpgradeStore) Candidates(ctx context.Context, selection ports.UpgradeSelection) ([]ports.UpgradeCandidate, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	rows, err := s.pool.Query(ctx, upgradeCandidatesSQL,
		selection.Limit, selection.Generation, selection.RetryAfter.Seconds(), selection.MaxQueued, selection.IncludeUnverifiedUploads)
	if err != nil {
		return nil, fmt.Errorf("select upgrade candidates: %w", err)
	}
	defer rows.Close()
	return collectUpgradeCandidates(rows)
}

func collectUpgradeCandidates(rows pgx.Rows) ([]ports.UpgradeCandidate, error) {
	var candidates []ports.UpgradeCandidate
	for rows.Next() {
		var trackID, userID uuid.UUID
		var grade string
		if err := rows.Scan(&trackID, &userID, &grade); err != nil {
			return nil, fmt.Errorf("scan upgrade candidate: %w", err)
		}
		candidates = append(candidates, ports.UpgradeCandidate{
			TrackID: shared.TrackIdFromUUID(trackID), UserID: shared.NewUserId(userID), Grade: grade,
		})
	}
	return candidates, rows.Err()
}

const noteUpgradeAttemptSQL = `
UPDATE tracks
SET acquisition_evidence = COALESCE(acquisition_evidence, '{}'::jsonb) || jsonb_build_object(
		'upgrade_tried_at', now(),
		'upgrade_tries', COALESCE((acquisition_evidence->>'upgrade_tries')::int, 0) + $3::int),
	rejected_source_keys = CASE
		WHEN $2::text = '' OR $2::text = ANY(COALESCE(rejected_source_keys, '{}')) THEN rejected_source_keys
		ELSE (array_append(COALESCE(rejected_source_keys, '{}'), $2::text))[
			greatest(cardinality(COALESCE(rejected_source_keys, '{}')) + 2 - $4::int, 1):]
	END,
	version = version + 1
WHERE id = $1
RETURNING (acquisition_evidence->>'upgrade_tries')::int`

const maxRejectedSourceKeys = 25

func (s *PgxUpgradeStore) NoteAttempt(ctx context.Context, trackID shared.TrackId, attempt ports.UpgradeAttempt) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	counted := 0
	if attempt.CountsAsTry {
		counted = 1
	}
	var tries int
	err := s.pool.QueryRow(ctx, noteUpgradeAttemptSQL,
		trackID.UUID(), attempt.RejectedSourceKey, counted, maxRejectedSourceKeys).Scan(&tries)
	if err != nil {
		return 0, fmt.Errorf("note upgrade attempt: %w", err)
	}
	return tries, nil
}

const giveUpUpgradeSQL = `
UPDATE tracks
SET acquisition_evidence = COALESCE(acquisition_evidence, '{}'::jsonb) || jsonb_build_object(
		'best_available_generation', $2::int,
		'best_available_reason', $3::text,
		'best_available_at', now(),
		'upgrade_tries', 0),
	version = version + 1
WHERE id = $1`

func (s *PgxUpgradeStore) GiveUp(ctx context.Context, trackID shared.TrackId, generation int, reason string) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	if _, err := s.pool.Exec(ctx, giveUpUpgradeSQL, trackID.UUID(), generation, reason); err != nil {
		return fmt.Errorf("give up upgrade: %w", err)
	}
	return nil
}

const gradeRankSQL = `array_position(ARRAY['F', 'D', 'C', 'B', 'A', 'A_user'], %s)`

var recordInPlaceSQL = fmt.Sprintf(`
UPDATE tracks
SET acquisition_evidence = COALESCE(acquisition_evidence, '{}'::jsonb) || jsonb_strip_nulls(jsonb_build_object(
		'in_place_generation', $3::int,
		'in_place_at', now(),
		'in_place_basis', NULLIF($5::text, ''),
		'in_place_bit_error_rate', NULLIF($9::float8, 0),
		'verdict', NULLIF($4::text, ''),
		'acoustid_score', NULLIF($6::float8, 0),
		'grade', CASE WHEN %s > %s THEN $7::text END,
		'user_upload_mismatch', CASE WHEN $8::boolean THEN true END)),
	version = version + 1
WHERE id = $1 AND audio_ref = $2 AND acquisition_status = 'ready'`,
	fmt.Sprintf(gradeRankSQL, "NULLIF($7::text, '')"),
	fmt.Sprintf(gradeRankSQL, "COALESCE(acquisition_evidence->>'grade', 'D')"))

func (s *PgxUpgradeStore) RecordInPlace(ctx context.Context, trackID shared.TrackId, record ports.InPlaceRecord) (bool, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	tag, err := s.pool.Exec(ctx, recordInPlaceSQL, trackID.UUID(), record.AudioRef, record.Generation,
		record.Verdict, record.Basis, record.Score, record.Grade, record.UserUploadMismatch, record.BitErrorRate)
	if err != nil {
		return false, fmt.Errorf("record in-place verdict: %w", err)
	}
	return tag.RowsAffected() == 1, nil
}

const noteInPlaceFailureSQL = `
UPDATE tracks
SET acquisition_evidence = COALESCE(acquisition_evidence, '{}'::jsonb) || jsonb_build_object(
		'upgrade_tried_at', now(),
		'in_place_failures', COALESCE((acquisition_evidence->>'in_place_failures')::int, 0) + 1),
	version = version + 1
WHERE id = $1
RETURNING (acquisition_evidence->>'in_place_failures')::int`

func (s *PgxUpgradeStore) NoteInPlaceFailure(ctx context.Context, trackID shared.TrackId) (int, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	var failures int
	if err := s.pool.QueryRow(ctx, noteInPlaceFailureSQL, trackID.UUID()).Scan(&failures); err != nil {
		return 0, fmt.Errorf("note in-place failure: %w", err)
	}
	return failures, nil
}
