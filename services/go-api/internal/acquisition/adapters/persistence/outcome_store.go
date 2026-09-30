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
INSERT INTO acquisition_outcomes (track_id, outcome, reason, elapsed_ms)
VALUES ($1, $2, $3, $4)`

func (s *PgxOutcomeStore) Record(ctx context.Context, o ports.AcquisitionOutcome) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	_, err := s.pool.Exec(ctx, recordOutcomeSQL, o.TrackID, o.Outcome, o.Reason, o.ElapsedMs)
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
