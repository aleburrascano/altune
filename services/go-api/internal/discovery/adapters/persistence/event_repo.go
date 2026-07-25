package persistence

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var _ ports.EventStore = (*PgxEventStore)(nil)
var _ ports.EventQuery = (*PgxEventStore)(nil)
var _ ports.BehavioralSignalStore = (*PgxEventStore)(nil)
var _ ports.BehavioralLabelStore = (*PgxEventStore)(nil)

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

	var queryNorm *string
	if event.QueryNorm != "" {
		queryNorm = &event.QueryNorm
	}

	var searchID *uuid.UUID
	if event.SearchId != "" {
		if id, parseErr := uuid.Parse(event.SearchId); parseErr == nil {
			searchID = &id
		}
	}

	var eventID *uuid.UUID
	if event.EventId != "" {
		if id, parseErr := uuid.Parse(event.EventId); parseErr == nil {
			eventID = &id
		} else {
			slog.DebugContext(ctx, "telemetry.event_id_unparseable",
				"event_id", event.EventId, "error", parseErr)
		}
	}

	var clientOccurredAt *time.Time
	if !event.ClientOccurredAt.IsZero() {
		t := event.ClientOccurredAt.UTC()
		clientOccurredAt = &t
	}

	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	_, err = r.pool.Exec(ctx,
		`INSERT INTO discovery_events
			(user_id, event_type, query_norm, search_id, event_id, client_occurred_at, payload, occurred_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (event_id) WHERE event_id IS NOT NULL DO NOTHING`,
		event.UserId.UUID(), event.Type.String(), queryNorm, searchID, eventID, clientOccurredAt, string(payloadJSON), occurredAt,
	)
	if err != nil {
		return fmt.Errorf("append telemetry event: %w", err)
	}
	return nil
}

func (r *PgxEventStore) ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT query_norm, COUNT(*) AS cnt
		FROM discovery_events
		WHERE event_type = $1
			AND occurred_at >= $2
			AND query_norm IS NOT NULL
			AND CASE WHEN jsonb_typeof(payload->'zero_result') = 'boolean'
				THEN (payload->>'zero_result')::boolean ELSE false END
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

func (r *PgxEventStore) NonZeroNoClickQueries(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT e.query_norm, COUNT(*) AS cnt
		FROM discovery_events e
		WHERE e.event_type = $1
			AND e.occurred_at >= $2
			AND e.query_norm IS NOT NULL
			AND CASE WHEN jsonb_typeof(e.payload->'zero_result') = 'boolean'
				THEN NOT (e.payload->>'zero_result')::boolean ELSE false END
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

const shortDwellThresholdMs = 20000

const perUserSignalCap = 3

func (r *PgxEventStore) SatisfactionSignals(ctx context.Context, since time.Time) ([]ports.BehavioralSignal, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT sig, SUM(user_score)::float8 AS score
		FROM (
			SELECT payload->>'result_signature' AS sig,
				user_id,
				LEAST(COUNT(*) FILTER (WHERE event_type IN ('play', 'completed')), $3)
				- LEAST(COUNT(*) FILTER (WHERE event_type = 'skip'
					AND CASE WHEN jsonb_typeof(payload->'dwell_ms') = 'number'
						THEN (payload->>'dwell_ms')::numeric < $2 ELSE false END), $3) AS user_score
			FROM discovery_events
			WHERE occurred_at >= $1
				AND event_type IN ('play', 'skip', 'completed')
				AND COALESCE(payload->>'result_signature', '') <> ''
			GROUP BY sig, user_id
		) per_user
		GROUP BY sig
		HAVING SUM(user_score) <> 0`,
		since, shortDwellThresholdMs, perUserSignalCap,
	)
	if err != nil {
		return nil, fmt.Errorf("query satisfaction signals: %w", err)
	}
	defer rows.Close()

	signals := []ports.BehavioralSignal{}
	for rows.Next() {
		var sig ports.BehavioralSignal
		if err := rows.Scan(&sig.ResultSignature, &sig.Score); err != nil {
			return nil, fmt.Errorf("scan satisfaction signal: %w", err)
		}
		signals = append(signals, sig)
	}
	return signals, rows.Err()
}

func (r *PgxEventStore) BehavioralLabels(ctx context.Context, since time.Time) ([]ports.BehavioralLabel, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT sp.query_norm,
			ev.payload->>'result_signature' AS sig,
			COALESCE(ev.payload->>'title', '') AS title,
			COALESCE(ev.payload->>'subtitle', ev.payload->>'artist', ev.payload->>'album', '') AS subtitle,
			MAX(CASE WHEN ev.event_type = 'wrong_album' THEN 1 ELSE 0 END) AS has_negative
		FROM discovery_events ev
		JOIN discovery_events sp
			ON sp.search_id = ev.search_id AND sp.event_type = 'search_performed'
		WHERE ev.occurred_at >= $1
			AND ev.search_id IS NOT NULL
			AND ev.event_type IN ('completed', 'library_add', 'wrong_album')
			AND COALESCE(ev.payload->>'result_signature', '') <> ''
			AND COALESCE(sp.query_norm, '') <> ''
		GROUP BY sp.query_norm, sig, title, subtitle`,
		since,
	)
	if err != nil {
		return nil, fmt.Errorf("query behavioral labels: %w", err)
	}
	defer rows.Close()

	labels := []ports.BehavioralLabel{}
	for rows.Next() {
		var (
			lbl         ports.BehavioralLabel
			hasNegative int
		)
		if err := rows.Scan(&lbl.QueryNorm, &lbl.ResultSignature, &lbl.Title, &lbl.Subtitle, &hasNegative); err != nil {
			return nil, fmt.Errorf("scan behavioral label: %w", err)
		}
		lbl.Polarity = 1
		if hasNegative == 1 {
			lbl.Polarity = -1
		}
		labels = append(labels, lbl)
	}
	return labels, rows.Err()
}

func (r *PgxEventStore) AbandonedSearches(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT sp.query_norm, COUNT(*) AS cnt
		FROM discovery_events sp
		WHERE sp.event_type = 'search_performed'
			AND sp.occurred_at >= $1
			AND sp.query_norm IS NOT NULL
			AND NOT EXISTS (
				SELECT 1 FROM discovery_events c
				WHERE c.event_type = 'result_clicked'
					AND c.search_id = sp.search_id
			)
			AND EXISTS (
				SELECT 1 FROM discovery_events nxt
				WHERE nxt.event_type = 'search_performed'
					AND nxt.payload->>'session_id' = sp.payload->>'session_id'
					AND sp.payload->>'session_id' IS NOT NULL
					AND nxt.occurred_at > sp.occurred_at
					AND nxt.occurred_at <= sp.occurred_at + interval '60 seconds'
			)
		GROUP BY sp.query_norm
		ORDER BY cnt DESC
		LIMIT $2`,
		since, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query abandoned searches: %w", err)
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
