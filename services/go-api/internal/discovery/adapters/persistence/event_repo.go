package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.EventStore = (*PgxEventStore)(nil)
var _ ports.EventQuery = (*PgxEventStore)(nil)

type PgxEventStore struct {
	pool *pgxpool.Pool
}

func NewPgxEventStore(pool *pgxpool.Pool) *PgxEventStore {
	return &PgxEventStore{pool: pool}
}

func (r *PgxEventStore) Append(ctx context.Context, event domain.InteractionEvent) error {
	payload := event.Payload
	if payload == nil {
		payload = map[string]any{}
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal telemetry payload: %w", err)
	}

	// query_norm is nullable — only search-originated events carry one.
	var queryNorm *string
	if event.QueryNorm != "" {
		queryNorm = &event.QueryNorm
	}

	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO discovery_events (user_id, event_type, query_norm, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5)`,
		event.UserId.UUID(), event.Type.String(), queryNorm, string(payloadJSON), occurredAt,
	)
	if err != nil {
		return fmt.Errorf("append telemetry event: %w", err)
	}
	return nil
}

// ZeroResultQueries ranks search_performed events flagged zero_result in the
// window by frequency. These are the strong coverage-gap candidates.
func (r *PgxEventStore) ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT query_norm, COUNT(*) AS cnt
		FROM discovery_events
		WHERE event_type = $1
			AND occurred_at >= $2
			AND query_norm IS NOT NULL
			AND (payload->>'zero_result')::boolean = true
		GROUP BY query_norm
		ORDER BY cnt DESC
		LIMIT $3`,
		domain.EventTypeSearchPerformed.String(), since, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query zero-result events: %w", err)
	}
	defer rows.Close()
	return scanQueryCounts(rows)
}

// NonZeroNoClickQueries ranks search_performed events that returned results but
// whose query_norm drew no click in the window — a weak coverage hint.
func (r *PgxEventStore) NonZeroNoClickQueries(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT e.query_norm, COUNT(*) AS cnt
		FROM discovery_events e
		WHERE e.event_type = $1
			AND e.occurred_at >= $2
			AND e.query_norm IS NOT NULL
			AND (e.payload->>'zero_result')::boolean = false
			AND NOT EXISTS (
				SELECT 1 FROM discovery_events c
				WHERE c.event_type = 'result_clicked'
					AND c.query_norm = e.query_norm
					AND c.occurred_at >= $2
			)
		GROUP BY e.query_norm
		ORDER BY cnt DESC
		LIMIT $3`,
		domain.EventTypeSearchPerformed.String(), since, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query no-click events: %w", err)
	}
	defer rows.Close()
	return scanQueryCounts(rows)
}

func scanQueryCounts(rows pgx.Rows) ([]ports.QueryCount, error) {
	counts := []ports.QueryCount{}
	for rows.Next() {
		var qc ports.QueryCount
		if err := rows.Scan(&qc.QueryNorm, &qc.Count); err != nil {
			return nil, fmt.Errorf("scan query count: %w", err)
		}
		counts = append(counts, qc)
	}
	return counts, rows.Err()
}
