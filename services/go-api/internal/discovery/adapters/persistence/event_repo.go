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

	// query_norm is nullable — only search-originated events carry one.
	var queryNorm *string
	if event.QueryNorm != "" {
		queryNorm = &event.QueryNorm
	}

	// search_id is nullable — only events that trace back to a search carry one.
	// Parse defensively: a malformed id is dropped (logged-not-swallowed upstream)
	// rather than failing the whole append.
	var searchID *uuid.UUID
	if event.SearchId != "" {
		if id, parseErr := uuid.Parse(event.SearchId); parseErr == nil {
			searchID = &id
		}
	}

	// event_id is the idempotency key — only the label-critical outbox tier sets
	// it; NULL otherwise, so fire-and-forget events never collide. A malformed
	// event_id is dropped (the event still lands, without dedup) — logged so the
	// silent loss of idempotency is at least observable.
	var eventID *uuid.UUID
	if event.EventId != "" {
		if id, parseErr := uuid.Parse(event.EventId); parseErr == nil {
			eventID = &id
		} else {
			slog.DebugContext(ctx, "telemetry.event_id_unparseable",
				"event_id", event.EventId, "error", parseErr)
		}
	}

	// client_occurred_at is nullable — only client-minted events carry one.
	var clientOccurredAt *time.Time
	if !event.ClientOccurredAt.IsZero() {
		t := event.ClientOccurredAt.UTC()
		clientOccurredAt = &t
	}

	occurredAt := event.OccurredAt
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}

	// received_at fills from its column default (now()). ON CONFLICT on the partial
	// event_id index makes a retried critical event a no-op (at-least-once → once).
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

// ZeroResultQueries ranks search_performed events flagged zero_result in the
// window by frequency. These are the strong coverage-gap candidates.
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

// NonZeroNoClickQueries ranks search_performed events that returned results but
// whose query_norm drew no click in the window — a weak coverage hint.
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

// shortDwellThresholdMs is the dwell below which a skip counts as
// dissatisfaction (Kim WSDM 2014: dwell <20–30s signals an unsatisfying result).
const shortDwellThresholdMs = 20000

// perUserSignalCap bounds one user's contribution per result_signature in each
// direction (positives and negatives capped separately, per user, per
// signature) so a single client replaying events cannot unboundedly pump or
// bury a result's behavioral score.
const perUserSignalCap = 3

// SatisfactionSignals aggregates play/skip/completed events per result_signature
// over the window into a net score: +1 per play (listen-threshold satisfaction)
// or completed (play-to-completion), −1 per skip whose dwell_ms is below the
// short-dwell threshold (skip-after-click dissatisfaction). A skip without a
// numeric dwell_ms counts 0 (unknown dwell is not evidence of dissatisfaction),
// and dwell_ms is read only when jsonb_typeof says it is a number — a poisoned
// payload ("dwell_ms":"abc") is skipped, never a 22P02 that bricks the whole
// window. The CASE wrapper (not a bare AND) guarantees the typeof guard is
// evaluated before the cast. result_signature rides in the JSONB payload
// (echoed by the client); only signed results are returned. Read-only
// analytics — never the request path.
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

// BehavioralLabels mines free relevance labels from query→engagement chains:
// each engagement event (completed, library_add, wrong_album) is joined to its
// originating search_performed by search_id to recover the query. A signature
// touched by a wrong_album is a hard negative (Polarity −1); otherwise a
// completed/library_add makes it a positive (Polarity +1). Read-only — never the
// request path.
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

// AbandonedSearches ranks queries that drew no click and were reformulated — the
// same session_id fired another search within 60s (a Joachims query-chain
// dissatisfaction signal). The no-click test joins precisely by search_id; the
// reformulation test joins by the session_id carried in the JSONB payload.
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
