package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type PgxOutcomeStore struct {
	pool *pgxpool.Pool
}

var _ ports.OutcomeRecorder = (*PgxOutcomeStore)(nil)

func NewPgxOutcomeStore(pool *pgxpool.Pool) *PgxOutcomeStore {
	return &PgxOutcomeStore{pool: pool}
}

const recordOutcomeSQL = `
INSERT INTO acquisition_outcomes (
	track_id, outcome, reason, elapsed_ms, stage_ms,
	source_host, source_format_id, source_codec, source_kbps, stored_bytes, stored_codec,
	job_kind, run, provenance, confidence, source_key, target_seconds, measured_seconds)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, NULLIF($9, 0), NULLIF($10, 0), $11,
	$12, NULLIF($13, 0), $14, NULLIF($15::double precision, 0), $16, NULLIF($17::double precision, 0), NULLIF($18::double precision, 0))`

func (s *PgxOutcomeStore) Record(ctx context.Context, o ports.AcquisitionOutcome) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	var stageMs map[string]int64
	if len(o.StageMs) > 0 {
		stageMs = o.StageMs
	}
	_, err := s.pool.Exec(ctx, recordOutcomeSQL, o.TrackID, string(o.Outcome), o.Reason, o.ElapsedMs, stageMs,
		o.SourceHost, o.Source.ID, o.Source.Codec, o.Source.Kbps, o.Stored.Bytes, o.Stored.Codec,
		string(o.Kind), o.Run, o.Provenance, o.Confidence, o.SourceKey, o.TargetSeconds, o.MeasuredSeconds)
	if err != nil {
		return fmt.Errorf("record outcome: %w", err)
	}
	return nil
}

const OutcomeRetention = 90 * 24 * time.Hour

const pruneOutcomesSQL = `
DELETE FROM acquisition_outcomes
WHERE id IN (
	SELECT id FROM acquisition_outcomes
	WHERE completed_at < $1
	LIMIT $2)`

func (s *PgxOutcomeStore) Prune(ctx context.Context, now time.Time) (int64, error) {
	pruned, err := pruneInBatches(ctx, s.pool, pruneOutcomesSQL, now.UTC().Add(-OutcomeRetention))
	if err != nil {
		return pruned, fmt.Errorf("prune acquisition outcomes: %w", err)
	}
	return pruned, nil
}
