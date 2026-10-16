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
INSERT INTO acquisition_rejections (track_id, source_key, reason, detail, expected_seconds, reference_mbid, rejected_at)
SELECT track_id, source_key, reason, detail, expected_seconds, reference_mbid, now()
FROM unnest($1::text[], $2::text[], $3::text[], $4::text[], $5::double precision[], $6::text[])
	AS u(track_id, source_key, reason, detail, expected_seconds, reference_mbid)
ON CONFLICT (track_id, source_key) DO UPDATE
	SET reason = EXCLUDED.reason,
		detail = EXCLUDED.detail,
		expected_seconds = EXCLUDED.expected_seconds,
		reference_mbid = EXCLUDED.reference_mbid,
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

type rejectionColumns struct {
	trackIDs        []string
	sourceKeys      []string
	reasons         []string
	details         []string
	expectedSeconds []float64
	referenceMBIDs  []string
}

func columnsOf(recs []ports.CandidateRejectionRecord) rejectionColumns {
	cols := rejectionColumns{
		trackIDs:        make([]string, len(recs)),
		sourceKeys:      make([]string, len(recs)),
		reasons:         make([]string, len(recs)),
		details:         make([]string, len(recs)),
		expectedSeconds: make([]float64, len(recs)),
		referenceMBIDs:  make([]string, len(recs)),
	}
	for i, r := range recs {
		cols.trackIDs[i] = r.TrackID
		cols.sourceKeys[i] = r.SourceKey
		cols.reasons[i] = r.Reason
		cols.details[i] = r.Detail
		cols.expectedSeconds[i] = r.Basis.ExpectedSeconds
		cols.referenceMBIDs[i] = r.Basis.ReferenceMBID
	}
	return cols
}

func (s *PgxRejectionStore) Record(ctx context.Context, recs []ports.CandidateRejectionRecord) error {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	if len(recs) == 0 {
		return nil
	}

	cols := columnsOf(dedupeLastWins(recs))
	_, err := s.pool.Exec(ctx, recordRejectionsSQL,
		cols.trackIDs, cols.sourceKeys, cols.reasons, cols.details, cols.expectedSeconds, cols.referenceMBIDs)
	if err != nil {
		return fmt.Errorf("record acquisition rejections: %w", err)
	}
	return nil
}

const activeRejectionsSQL = `
SELECT source_key, expected_seconds, reference_mbid FROM acquisition_rejections
WHERE track_id = $1 AND rejected_at >= $2
ORDER BY source_key`

func scanActiveRejections(rows pgx.Rows) ([]ports.PriorRejection, error) {
	var prior []ports.PriorRejection
	for rows.Next() {
		var key string
		var expectedSeconds *float64
		var referenceMBID *string
		if err := rows.Scan(&key, &expectedSeconds, &referenceMBID); err != nil {
			return nil, fmt.Errorf("scan active acquisition rejection: %w", err)
		}
		prior = append(prior, ports.PriorRejection{SourceKey: key, Basis: recordedBasis(expectedSeconds, referenceMBID)})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active acquisition rejections: %w", err)
	}
	return prior, nil
}

func recordedBasis(expectedSeconds *float64, referenceMBID *string) *ports.RejectionBasis {
	if expectedSeconds == nil || referenceMBID == nil {
		return nil
	}
	return &ports.RejectionBasis{ExpectedSeconds: *expectedSeconds, ReferenceMBID: *referenceMBID}
}

func (s *PgxRejectionStore) Active(ctx context.Context, trackID string, since time.Time) ([]ports.PriorRejection, error) {
	ctx, cancel := context.WithTimeout(ctx, dbCallTimeout)
	defer cancel()

	rows, err := s.pool.Query(ctx, activeRejectionsSQL, trackID, since)
	if err != nil {
		return nil, fmt.Errorf("list active acquisition rejections: %w", err)
	}
	defer rows.Close()

	return scanActiveRejections(rows)
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
