package persistence

import (
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/clock"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	_ ports.EventStore               = (*PostgresEventStore)(nil)
	_ ports.EventQuery               = (*PostgresEventStore)(nil)
	_ ports.ListeningSignalStore     = (*PostgresEventStore)(nil)
	_ ports.DiscographyQualityReader = (*PostgresEventStore)(nil)
	_ ports.DeletedUserDataEraser    = (*PostgresEventStore)(nil)
)

type PostgresEventStore struct {
	pool *pgxpool.Pool
	now  clock.Func
}

func NewPostgresEventStore(pool *pgxpool.Pool, now clock.Func) *PostgresEventStore {
	if now == nil {
		panic("persistence.NewPostgresEventStore: a clock is required")
	}
	return &PostgresEventStore{pool: pool, now: now}
}

const appendEventSQL = `INSERT INTO discovery_events
		(user_id, event_type, query_norm, search_id, event_id, client_occurred_at, payload, occurred_at)
	VALUES ($1, $2::text,
		CASE WHEN $2::text = $9::text THEN (
			CASE WHEN EXISTS (
				SELECT 1 FROM discovery_search_history_cleared c
				WHERE c.user_id = $1 AND c.cleared_at > $8
			) THEN NULL ELSE $3::text END
		) ELSE (
			SELECT sp.query_norm FROM discovery_events sp
			WHERE sp.search_id = $4::uuid
				AND sp.user_id = $1
				AND sp.event_type = $9::text
			ORDER BY sp.occurred_at
			LIMIT 1
		) END,
		$4::uuid, $5, $6, $7, $8)
	ON CONFLICT (user_id, event_id) WHERE event_id IS NOT NULL DO NOTHING`

func (r *PostgresEventStore) Append(ctx context.Context, event domain.InteractionEvent) error {
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
		occurredAt = r.now().UTC()
	}

	err = execUnderSearchTextLock(ctx, r.pool, event.UserId.UUID(), appendEventSQL,
		event.UserId.UUID(), event.Type.String(), queryNorm, searchID, eventID, clientOccurredAt, string(payloadJSON), occurredAt,
		domain.EventTypeSearchPerformed.String(),
	)
	if err != nil {
		return fmt.Errorf("append telemetry event: %w", err)
	}
	return nil
}

var zeroResultQueriesSQL = fmt.Sprintf(`SELECT query_norm, COUNT(*) AS cnt
	FROM discovery_events
	WHERE event_type = $1
		AND occurred_at >= $2
		AND query_norm IS NOT NULL
		AND CASE WHEN jsonb_typeof(payload->'%[1]s') = 'boolean'
			THEN (payload->>'%[1]s')::boolean ELSE false END
	GROUP BY query_norm
	ORDER BY cnt DESC
	LIMIT $3`, domain.PayloadKeyZeroResult)

func (r *PostgresEventStore) NoResultSearches(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	rows, err := r.pool.Query(ctx, zeroResultQueriesSQL,
		domain.EventTypeSearchPerformed.String(), since, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query zero-result events: %w", err)
	}
	return scanQueryCounts(rows)
}

var zeroResultTotalSQL = fmt.Sprintf(`SELECT COUNT(*)
	FROM discovery_events
	WHERE event_type = $1
		AND occurred_at >= $2
		AND query_norm IS NOT NULL
		AND CASE WHEN jsonb_typeof(payload->'%[1]s') = 'boolean'
			THEN (payload->>'%[1]s')::boolean ELSE false END`, domain.PayloadKeyZeroResult)

func (r *PostgresEventStore) ZeroResultTotal(ctx context.Context, since time.Time) (int, error) {
	var total int
	err := r.pool.QueryRow(ctx, zeroResultTotalSQL,
		domain.EventTypeSearchPerformed.String(), since,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("count zero-result events: %w", err)
	}
	return total, nil
}

var nonZeroNoClickQueriesSQL = fmt.Sprintf(`SELECT e.query_norm, COUNT(*) AS cnt
	FROM discovery_events e
	WHERE e.event_type = $1
		AND e.occurred_at >= $2
		AND e.query_norm IS NOT NULL
		AND CASE WHEN jsonb_typeof(e.payload->'%[1]s') = 'boolean'
			THEN NOT (e.payload->>'%[1]s')::boolean ELSE false END
		AND NOT EXISTS (
			SELECT 1 FROM discovery_events c
			JOIN discovery_events s
				ON s.search_id = c.search_id
				AND s.user_id = c.user_id
				AND s.event_type = $1
			WHERE c.event_type = $4
				AND s.query_norm = e.query_norm
				AND c.occurred_at >= $2
		)
	GROUP BY e.query_norm
	ORDER BY cnt DESC
	LIMIT $3`, domain.PayloadKeyZeroResult)

func (r *PostgresEventStore) NonZeroNoClickQueries(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	rows, err := r.pool.Query(ctx, nonZeroNoClickQueriesSQL,
		domain.EventTypeSearchPerformed.String(), since, limit,
		domain.EventTypeResultClicked.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("query no-click events: %w", err)
	}
	return scanQueryCounts(rows)
}

const shortDwellThresholdMs = 20000

const perUserSignalCap = 3

const shownResultWindow = 24 * time.Hour

var satisfactionSignalsSQL = fmt.Sprintf(`SELECT sig, SUM(user_score)::float8 AS score
	FROM (
		SELECT ev.payload->>'%[1]s' AS sig,
			ev.user_id,
			LEAST(COUNT(*) FILTER (WHERE ev.event_type IN ($5, $7)), $3)
			- LEAST(COUNT(*) FILTER (WHERE ev.event_type = $6
				AND CASE WHEN jsonb_typeof(ev.payload->'%[2]s') = 'number'
					THEN (ev.payload->>'%[2]s')::numeric < $2 ELSE false END), $3) AS user_score
		FROM discovery_events ev
		WHERE ev.occurred_at >= $1
			AND ev.event_type IN ($5, $6, $7)
			AND COALESCE(ev.payload->>'%[1]s', '') <> ''
			AND EXISTS (
				SELECT 1 FROM discovery_events sp
				WHERE sp.user_id = ev.user_id
					AND sp.event_type = $8
					AND sp.occurred_at <= ev.occurred_at + interval '1 minute'
					AND sp.occurred_at >= ev.occurred_at - ($4 * interval '1 second')
					AND sp.payload->'%[3]s' @> jsonb_build_array(ev.payload->>'%[1]s')
			)
		GROUP BY sig, ev.user_id
	) per_user
	GROUP BY sig
	HAVING SUM(user_score) <> 0`,
	domain.PayloadKeyResultSignature, domain.PayloadKeyDwellMs, domain.PayloadKeyShownSignatures)

func (r *PostgresEventStore) SatisfactionSignals(ctx context.Context, since time.Time) ([]ports.BehavioralSignal, error) {
	rows, err := r.pool.Query(ctx, satisfactionSignalsSQL,
		since, shortDwellThresholdMs, perUserSignalCap, int64(shownResultWindow/time.Second),
		domain.EventTypePlay.String(),
		domain.EventTypeSkip.String(),
		domain.EventTypeCompleted.String(),
		domain.EventTypeSearchPerformed.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("query satisfaction signals: %w", err)
	}
	collected, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ports.BehavioralSignal, error) {
		var sig ports.BehavioralSignal
		if err := row.Scan(&sig.ResultSignature, &sig.Score); err != nil {
			return sig, fmt.Errorf("scan satisfaction signal: %w", err)
		}
		return sig, nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect satisfaction signals: %w", err)
	}
	return collected, nil
}

const abandonedSearchesSQL = `SELECT sp.query_norm, COUNT(*) AS cnt
	FROM discovery_events sp
	WHERE sp.event_type = $3
		AND sp.occurred_at >= $1
		AND sp.query_norm IS NOT NULL
		AND NOT EXISTS (
			SELECT 1 FROM discovery_events c
			WHERE c.event_type = $4
				AND c.search_id = sp.search_id
		)
		AND EXISTS (
			SELECT 1 FROM discovery_events nxt
			WHERE nxt.event_type = $3
				AND nxt.user_id = sp.user_id
				AND NOT starts_with(nxt.query_norm, sp.query_norm)
				AND NOT starts_with(sp.query_norm, nxt.query_norm)
				AND nxt.occurred_at > sp.occurred_at
				AND nxt.occurred_at <= sp.occurred_at + interval '60 seconds'
		)
	GROUP BY sp.query_norm
	ORDER BY cnt DESC
	LIMIT $2`

func (r *PostgresEventStore) AbandonedSearches(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	rows, err := r.pool.Query(ctx, abandonedSearchesSQL,
		since, limit,
		domain.EventTypeSearchPerformed.String(),
		domain.EventTypeResultClicked.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("query abandoned searches: %w", err)
	}
	return scanQueryCounts(rows)
}

const eraseEventsOfDeletedIdentitiesSQL = `
	DELETE FROM discovery_events e
	WHERE EXISTS (SELECT 1 FROM auth_user_ids)
	  AND e.user_id <> $1
	  AND NOT EXISTS (SELECT 1 FROM auth_user_ids u WHERE u.id = e.user_id)`

func (r *PostgresEventStore) EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error) {
	return eraseRowsOfDeletedIdentities(ctx, r.pool,
		"erase events of deleted identities", eraseEventsOfDeletedIdentitiesSQL)
}

const eraseEventSearchTextOfUserSQL = `
	UPDATE discovery_events
	SET query_norm = NULL
	WHERE user_id = $1 AND query_norm IS NOT NULL`

func scanQueryCounts(rows pgx.Rows) ([]ports.QueryCount, error) {
	collected, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (ports.QueryCount, error) {
		var qc ports.QueryCount
		if err := row.Scan(&qc.QueryNorm, &qc.Count); err != nil {
			return qc, fmt.Errorf("scan query count: %w", err)
		}
		return qc, nil
	})
	if err != nil {
		return nil, fmt.Errorf("collect query counts: %w", err)
	}
	return collected, nil
}
