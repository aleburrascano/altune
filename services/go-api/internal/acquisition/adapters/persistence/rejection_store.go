package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PgxRejectionStore struct {
	pool *pgxpool.Pool
}

var _ ports.RejectionStore = (*PgxRejectionStore)(nil)

func NewPgxRejectionStore(pool *pgxpool.Pool) *PgxRejectionStore {
	return &PgxRejectionStore{pool: pool}
}

const recordRejectionsSQL = `
INSERT INTO acquisition_rejections (track_id, source_key, reason, detail, rejected_at)
SELECT track_id, source_key, reason, detail, now()
FROM unnest($1::text[], $2::text[], $3::text[], $4::text[])
	AS u(track_id, source_key, reason, detail)
ON CONFLICT (track_id, source_key) DO UPDATE
	SET reason = EXCLUDED.reason,
		detail = EXCLUDED.detail,
		rejected_at = EXCLUDED.rejected_at`

type rejectionBatchKey struct {
	trackID   string
	sourceKey string
}

func dedupeLastWins(recs []ports.CandidateRejectionRecord) []ports.CandidateRejectionRecord {
	order := make([]rejectionBatchKey, 0, len(recs))
	byKey := make(map[rejectionBatchKey]ports.CandidateRejectionRecord, len(recs))
	for _, r := range recs {
		key := rejectionBatchKey{trackID: r.TrackID, sourceKey: r.SourceKey}
		if _, seen := byKey[key]; !seen {
			order = append(order, key)
		}
		byKey[key] = r
	}
	deduped := make([]ports.CandidateRejectionRecord, len(order))
	for i, key := range order {
		deduped[i] = byKey[key]
	}
	return deduped
}

func columnsOf(recs []ports.CandidateRejectionRecord) (trackIDs, sourceKeys, reasons, details []string) {
	trackIDs = make([]string, len(recs))
	sourceKeys = make([]string, len(recs))
	reasons = make([]string, len(recs))
	details = make([]string, len(recs))
	for i, r := range recs {
		trackIDs[i] = r.TrackID
		sourceKeys[i] = r.SourceKey
		reasons[i] = r.Reason
		details[i] = r.Detail
	}
	return trackIDs, sourceKeys, reasons, details
}

func (s *PgxRejectionStore) Record(ctx context.Context, recs []ports.CandidateRejectionRecord) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	if len(recs) == 0 {
		return nil
	}

	trackIDs, sourceKeys, reasons, details := columnsOf(dedupeLastWins(recs))
	_, err := s.pool.Exec(ctx, recordRejectionsSQL, trackIDs, sourceKeys, reasons, details)
	if err != nil {
		return fmt.Errorf("record acquisition rejections: %w", err)
	}
	return nil
}

const activeRejectionKeysSQL = `
SELECT source_key FROM acquisition_rejections
WHERE track_id = $1 AND rejected_at >= $2
ORDER BY source_key`

func scanActiveKeys(rows pgx.Rows) ([]string, error) {
	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("scan active acquisition rejection: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active acquisition rejections: %w", err)
	}
	return keys, nil
}

func (s *PgxRejectionStore) ActiveKeys(ctx context.Context, trackID string, since time.Time) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	rows, err := s.pool.Query(ctx, activeRejectionKeysSQL, trackID, since)
	if err != nil {
		return nil, fmt.Errorf("list active acquisition rejections: %w", err)
	}
	defer rows.Close()

	return scanActiveKeys(rows)
}

const RejectionRetention = 60 * 24 * time.Hour

const pruneBatchSize = 1000

const pruneRejectionsSQL = `
DELETE FROM acquisition_rejections
WHERE ctid IN (
	SELECT ctid FROM acquisition_rejections
	WHERE rejected_at < $1
	LIMIT $2)`

func pruneInBatches(ctx context.Context, pool *pgxpool.Pool, sql string, cutoff time.Time) (int64, error) {
	var total int64
	for {
		batchCtx, cancel := context.WithTimeout(ctx, dbCallTimeout)
		tag, err := pool.Exec(batchCtx, sql, cutoff, pruneBatchSize)
		cancel()
		if err != nil {
			return total, err
		}
		total += tag.RowsAffected()
		if tag.RowsAffected() < pruneBatchSize {
			return total, nil
		}
	}
}

func (s *PgxRejectionStore) Prune(ctx context.Context, now time.Time) (int64, error) {
	pruned, err := pruneInBatches(ctx, s.pool, pruneRejectionsSQL, now.UTC().Add(-RejectionRetention))
	if err != nil {
		return pruned, fmt.Errorf("prune acquisition rejections: %w", err)
	}
	return pruned, nil
}
