package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
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
	_ ports.EventStore               = (*PgxEventStore)(nil)
	_ ports.EventQuery               = (*PgxEventStore)(nil)
	_ ports.BehavioralSignalStore    = (*PgxEventStore)(nil)
	_ ports.BehavioralLabelStore     = (*PgxEventStore)(nil)
	_ ports.DiscographyQualityReader = (*PgxEventStore)(nil)
	_ ports.DiscographyPruner        = (*PgxEventStore)(nil)
	_ ports.DeletedIdentityEraser    = (*PgxEventStore)(nil)
)

type PgxEventStore struct {
	pool *pgxpool.Pool
}

func NewPgxEventStore(pool *pgxpool.Pool) *PgxEventStore {
	return &PgxEventStore{pool: pool}
}

// appendEventSQL stores event.QueryNorm only on the server-emitted
// search_performed row. Every other event's query_norm is resolved from the
// same user's search_performed row for its search_id (NULL when there is none),
// so a client-chosen value can never enter the coverage-gap joins (#1086).
//
// The conflict target carries user_id because event_id alone is the caller's to
// choose: scoped to its author, a replayed id can no-op only that author's own
// retry, never another user's event (#2245, migration 024).
const appendEventSQL = `INSERT INTO discovery_events
		(user_id, event_type, query_norm, search_id, event_id, client_occurred_at, payload, occurred_at)
	VALUES ($1, $2::text,
		CASE WHEN $2::text = $9::text THEN $3::text ELSE (
			SELECT sp.query_norm FROM discovery_events sp
			WHERE sp.search_id = $4::uuid
				AND sp.user_id = $1
				AND sp.event_type = $9::text
			ORDER BY sp.occurred_at
			LIMIT 1
		) END,
		$4::uuid, $5, $6, $7, $8)
	ON CONFLICT (user_id, event_id) WHERE event_id IS NOT NULL DO NOTHING`

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

	_, err = r.pool.Exec(ctx, appendEventSQL,
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

func (r *PgxEventStore) ZeroResultQueries(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	rows, err := r.pool.Query(ctx, zeroResultQueriesSQL,
		domain.EventTypeSearchPerformed.String(), since, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query zero-result events: %w", err)
	}
	defer rows.Close()
	return scanQueryCounts(rows)
}

var zeroResultTotalSQL = fmt.Sprintf(`SELECT COUNT(*)
	FROM discovery_events
	WHERE event_type = $1
		AND occurred_at >= $2
		AND query_norm IS NOT NULL
		AND CASE WHEN jsonb_typeof(payload->'%[1]s') = 'boolean'
			THEN (payload->>'%[1]s')::boolean ELSE false END`, domain.PayloadKeyZeroResult)

// ZeroResultTotal counts every zero-result search in the window, unbounded by
// the top-N cap of ZeroResultQueries. The list is truncated at a LIMIT for
// display; this true total is what threshold comparisons must use so a window
// spanning more than that many distinct normalized queries is not undercounted.
func (r *PgxEventStore) ZeroResultTotal(ctx context.Context, since time.Time) (int, error) {
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

// NonZeroNoClickQueries reports non-zero searches whose query was never
// clicked. A click is attributed to a query through its search_id's
// search_performed row, never through the click row's own query_norm, so the
// signal holds for clicks recorded before that row landed or before #1086.
func (r *PgxEventStore) NonZeroNoClickQueries(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	rows, err := r.pool.Query(ctx, nonZeroNoClickQueriesSQL,
		domain.EventTypeSearchPerformed.String(), since, limit,
		domain.EventTypeResultClicked.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("query no-click events: %w", err)
	}
	defer rows.Close()
	return scanQueryCounts(rows)
}

const shortDwellThresholdMs = 20000

const perUserSignalCap = 3

// shownResultWindow bounds how long after a search a shown result may still
// earn a satisfaction signal from the user it was shown to.
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

// SatisfactionSignals aggregates play/skip/completed events into a global
// per-signature score. A result_signature is computable offline, so an event
// only counts when the same user was shown that signature by a server-emitted
// search_performed event within shownResultWindow before it (#573).
func (r *PgxEventStore) SatisfactionSignals(ctx context.Context, since time.Time) ([]ports.BehavioralSignal, error) {
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
	defer rows.Close()

	return collectRows(rows, func(rows pgx.Rows) (ports.BehavioralSignal, error) {
		var sig ports.BehavioralSignal
		if err := rows.Scan(&sig.ResultSignature, &sig.Score); err != nil {
			return sig, fmt.Errorf("scan satisfaction signal: %w", err)
		}
		return sig, nil
	})
}

// behavioralLabelsSQL reads title and subtitle straight as literals: unlike the
// keys below, they are client-submitted display fields with no Go definition to
// name them by.
var behavioralLabelsSQL = fmt.Sprintf(`SELECT sp.query_norm,
		ev.payload->>'%[1]s' AS sig,
		COALESCE(ev.payload->>'title', '') AS title,
		COALESCE(ev.payload->>'subtitle', ev.payload->>'artist', ev.payload->>'album', '') AS subtitle,
		MAX(CASE WHEN ev.event_type = $4 THEN 1 ELSE 0 END) AS has_negative
	FROM discovery_events ev
	JOIN discovery_events sp
		ON sp.search_id = ev.search_id AND sp.event_type = $5
	WHERE ev.occurred_at >= $1
		AND ev.search_id IS NOT NULL
		AND ev.event_type IN ($2, $3, $4)
		AND COALESCE(ev.payload->>'%[1]s', '') <> ''
		AND COALESCE(sp.query_norm, '') <> ''
	GROUP BY sp.query_norm, sig, title, subtitle`, domain.PayloadKeyResultSignature)

func (r *PgxEventStore) BehavioralLabels(ctx context.Context, since time.Time) ([]ports.BehavioralLabel, error) {
	rows, err := r.pool.Query(ctx, behavioralLabelsSQL,
		since,
		domain.EventTypeCompleted.String(),
		domain.EventTypeLibraryAdd.String(),
		domain.EventTypeWrongAlbum.String(),
		domain.EventTypeSearchPerformed.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("query behavioral labels: %w", err)
	}
	defer rows.Close()

	return collectRows(rows, func(rows pgx.Rows) (ports.BehavioralLabel, error) {
		var (
			lbl         ports.BehavioralLabel
			hasNegative int
		)
		if err := rows.Scan(&lbl.QueryNorm, &lbl.ResultSignature, &lbl.Title, &lbl.Subtitle, &hasNegative); err != nil {
			return lbl, fmt.Errorf("scan behavioral label: %w", err)
		}
		lbl.Polarity = 1
		if hasNegative == 1 {
			lbl.Polarity = -1
		}
		return lbl, nil
	})
}

var abandonedSearchesSQL = fmt.Sprintf(`SELECT sp.query_norm, COUNT(*) AS cnt
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
				AND nxt.payload->>'%[1]s' = sp.payload->>'%[1]s'
				AND sp.payload->>'%[1]s' IS NOT NULL
				AND nxt.occurred_at > sp.occurred_at
				AND nxt.occurred_at <= sp.occurred_at + interval '60 seconds'
		)
	GROUP BY sp.query_norm
	ORDER BY cnt DESC
	LIMIT $2`, domain.PayloadKeySessionId)

func (r *PgxEventStore) AbandonedSearches(ctx context.Context, since time.Time, limit int) ([]ports.QueryCount, error) {
	rows, err := r.pool.Query(ctx, abandonedSearchesSQL,
		since, limit,
		domain.EventTypeSearchPerformed.String(),
		domain.EventTypeResultClicked.String(),
	)
	if err != nil {
		return nil, fmt.Errorf("query abandoned searches: %w", err)
	}
	defer rows.Close()
	return scanQueryCounts(rows)
}

// eraseEventsOfDeletedIdentitiesSQL drops the telemetry of accounts whose
// identity is gone. The per-type retention prune already bounds the table by
// age, but its widest window is 400 days, so without this a deleted account's
// events — its search terms, the results it was shown, what it played — survive
// the account by that long.
//
// $1 is the synthetic system identity, and this table is the reason it has to be
// excluded: discography_observed rows are server-emitted under it on purpose, so
// they belong to no account and are not an account's to erase.
//
// Cost: one pass over discovery_events per run, each row probing auth.users'
// primary key. This is the widest of the three tables, and the reason the
// sweep's hourly cadence is the ceiling rather than something finer.
const eraseEventsOfDeletedIdentitiesSQL = `
	DELETE FROM discovery_events e
	WHERE EXISTS (SELECT 1 FROM auth.users)
	  AND e.user_id <> $1
	  AND NOT EXISTS (SELECT 1 FROM auth.users u WHERE u.id = e.user_id)`

func (r *PgxEventStore) EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error) {
	return eraseRowsOfDeletedIdentities(ctx, r.pool,
		"erase events of deleted identities", eraseEventsOfDeletedIdentitiesSQL)
}

func scanQueryCounts(rows pgx.Rows) ([]ports.QueryCount, error) {
	return collectRows(rows, func(rows pgx.Rows) (ports.QueryCount, error) {
		var qc ports.QueryCount
		if err := rows.Scan(&qc.QueryNorm, &qc.Count); err != nil {
			return qc, fmt.Errorf("scan query count: %w", err)
		}
		return qc, nil
	})
}
