package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
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
const appendEventSQL = `INSERT INTO discovery_events
		(user_id, event_type, query_norm, search_id, event_id, client_occurred_at, payload, occurred_at)
	VALUES ($1, $2::text,
		CASE WHEN $2::text = 'search_performed' THEN $3::text ELSE (
			SELECT sp.query_norm FROM discovery_events sp
			WHERE sp.search_id = $4::uuid
				AND sp.user_id = $1
				AND sp.event_type = 'search_performed'
			ORDER BY sp.occurred_at
			LIMIT 1
		) END,
		$4::uuid, $5, $6, $7, $8)
	ON CONFLICT (event_id) WHERE event_id IS NOT NULL DO NOTHING`

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
	)
	if err != nil {
		return fmt.Errorf("append telemetry event: %w", err)
	}
	return nil
}

// discographyRetentionWindow bounds how long a discography_observed event is kept
// before the periodic prune evicts it. It stays strictly greater than
// maxQualityWindowDays (365, in internal/admin/handler) — the widest window the
// aggregate can be asked to read — so the prune can never remove a row a
// legitimate window_days query could still scan. The margin over that cap absorbs
// clock skew and tick lag between the prune's clock and a reader's, so no
// in-window row is evicted even at a clock edge.
const discographyRetentionWindow = 400 * 24 * time.Hour

// pruneEventsByTypeSQL evicts one event type's rows strictly older than the
// cutoff. It is keyed on (event_type, occurred_at), served by
// idx_discovery_events_type_time, so the delete touches only the tail it removes.
// The prune is always keyed on a single type against that type's own cutoff — a
// blanket table-wide age-prune would evict rows of a type read over a wide window
// using another type's narrow one, so retention is enforced strictly per type.
const pruneEventsByTypeSQL = `DELETE FROM discovery_events
	WHERE event_type = $1 AND occurred_at < $2`

// aggregateEventRetention bounds how long the discovery_events types that feed a
// windowed aggregate are kept. The widest window any of them is read over is the
// 30-day behavioral/corpus/coverage lookback (a satisfaction join reaches ~24h
// further back through shownResultWindow); 90 days keeps a ~3x margin over that,
// so the prune can never evict a row a live read could still scan. It also bounds
// the effective history the offline coverage/behavioral eval (-since-days) can
// surface, the same role maxQualityWindowDays plays for discography.
const aggregateEventRetention = 90 * 24 * time.Hour

// AggregateEventRetention exposes aggregateEventRetention to the offline eval CLI
// so it can clamp its -since-days read to the window the prune actually keeps,
// the same role maxQualityWindowDays plays for the discography read path. One
// source of truth for the ceiling: the value that bounds eviction is the value
// that bounds reads.
const AggregateEventRetention = aggregateEventRetention

// writeOnlyEventRetention bounds the discovery_events types no aggregate reads
// (results_shown, search_failed, search_degraded, playback_health). Their read
// window is zero, so any positive retention is safe; 30 days bounds their growth
// while leaving an operator a month of raw telemetry to inspect.
const writeOnlyEventRetention = 30 * 24 * time.Hour

// eventRetention is the per-type retention policy for every persisted
// discovery_events type except discography_observed, which owns the wider
// discographyRetentionWindow (tied to its 365-day read cap) and its own eviction.
// Each window is strictly wider than the widest window any aggregate reads that
// type over, so pruning a type can never remove a row another type's read — or its
// own — could still serve.
var eventRetention = []struct {
	eventType domain.EventType
	window    time.Duration
}{
	{domain.EventTypeSearchPerformed, aggregateEventRetention},
	{domain.EventTypeResultClicked, aggregateEventRetention},
	{domain.EventTypePlay, aggregateEventRetention},
	{domain.EventTypeSkip, aggregateEventRetention},
	{domain.EventTypeCompleted, aggregateEventRetention},
	{domain.EventTypeLibraryAdd, aggregateEventRetention},
	{domain.EventTypeWrongAlbum, aggregateEventRetention},
	{domain.EventTypeResultsShown, writeOnlyEventRetention},
	{domain.EventTypeSearchFailed, writeOnlyEventRetention},
	{domain.EventTypeSearchDegraded, writeOnlyEventRetention},
	{domain.EventTypePlaybackHealth, writeOnlyEventRetention},
}

// PruneEvents evicts every non-discography event type older than that type's own
// retention window measured back from now, returning the total rows removed. Each
// type is deleted against its own cutoff, so a type read over a wide window is
// never evicted by one read over a narrow window. It is idempotent: each run
// re-evaluates the whole tail against the current cutoff, so a missed run defers
// eviction without ever skipping a row. discography_observed is pruned separately
// by PruneDiscographyObserved, which owns its wider window.
func (r *PgxEventStore) PruneEvents(ctx context.Context, now time.Time) (int64, error) {
	var total int64
	for _, ret := range eventRetention {
		cutoff := now.UTC().Add(-ret.window)
		tag, err := r.pool.Exec(ctx, pruneEventsByTypeSQL, ret.eventType.String(), cutoff)
		if err != nil {
			return total, fmt.Errorf("prune %s events: %w", ret.eventType, err)
		}
		total += tag.RowsAffected()
	}
	return total, nil
}

// PruneDiscographyObserved evicts discography_observed events older than the
// retention window measured back from now, returning the rows removed. The cutoff
// (now - discographyRetentionWindow) is always older than the widest readable
// window, so the prune bounds the table's growth on every discography open without
// ever removing a row the aggregate could still serve. It is idempotent: a missed
// run defers eviction but never skips a row, because each run re-evaluates the
// whole tail against the current cutoff rather than a since-last-run slice.
func (r *PgxEventStore) PruneDiscographyObserved(ctx context.Context, now time.Time) (int64, error) {
	cutoff := now.UTC().Add(-discographyRetentionWindow)
	tag, err := r.pool.Exec(ctx, pruneEventsByTypeSQL,
		domain.EventTypeDiscographyObserved.String(), cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("prune discography events: %w", err)
	}
	return tag.RowsAffected(), nil
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

// ZeroResultTotal counts every zero-result search in the window, unbounded by
// the top-N cap of ZeroResultQueries. The list is truncated at a LIMIT for
// display; this true total is what threshold comparisons must use so a window
// spanning more than that many distinct normalized queries is not undercounted.
func (r *PgxEventStore) ZeroResultTotal(ctx context.Context, since time.Time) (int, error) {
	var total int
	err := r.pool.QueryRow(ctx,
		`SELECT COUNT(*)
		FROM discovery_events
		WHERE event_type = $1
			AND occurred_at >= $2
			AND query_norm IS NOT NULL
			AND CASE WHEN jsonb_typeof(payload->'zero_result') = 'boolean'
				THEN (payload->>'zero_result')::boolean ELSE false END`,
		domain.EventTypeSearchPerformed.String(), since,
	).Scan(&total)
	if err != nil {
		return 0, fmt.Errorf("count zero-result events: %w", err)
	}
	return total, nil
}

// NonZeroNoClickQueries reports non-zero searches whose query was never
// clicked. A click is attributed to a query through its search_id's
// search_performed row, never through the click row's own query_norm, so the
// signal holds for clicks recorded before that row landed or before #1086.
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
				JOIN discovery_events s
					ON s.search_id = c.search_id
					AND s.user_id = c.user_id
					AND s.event_type = $1
				WHERE c.event_type = 'result_clicked'
					AND s.query_norm = e.query_norm
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

// shownResultWindow bounds how long after a search a shown result may still
// earn a satisfaction signal from the user it was shown to.
const shownResultWindow = 24 * time.Hour

// SatisfactionSignals aggregates play/skip/completed events into a global
// per-signature score. A result_signature is computable offline, so an event
// only counts when the same user was shown that signature by a server-emitted
// search_performed event within shownResultWindow before it (#573).
func (r *PgxEventStore) SatisfactionSignals(ctx context.Context, since time.Time) ([]ports.BehavioralSignal, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT sig, SUM(user_score)::float8 AS score
		FROM (
			SELECT ev.payload->>'result_signature' AS sig,
				ev.user_id,
				LEAST(COUNT(*) FILTER (WHERE ev.event_type IN ('play', 'completed')), $3)
				- LEAST(COUNT(*) FILTER (WHERE ev.event_type = 'skip'
					AND CASE WHEN jsonb_typeof(ev.payload->'dwell_ms') = 'number'
						THEN (ev.payload->>'dwell_ms')::numeric < $2 ELSE false END), $3) AS user_score
			FROM discovery_events ev
			WHERE ev.occurred_at >= $1
				AND ev.event_type IN ('play', 'skip', 'completed')
				AND COALESCE(ev.payload->>'result_signature', '') <> ''
				AND EXISTS (
					SELECT 1 FROM discovery_events sp
					WHERE sp.user_id = ev.user_id
						AND sp.event_type = 'search_performed'
						AND sp.occurred_at <= ev.occurred_at + interval '1 minute'
						AND sp.occurred_at >= ev.occurred_at - ($4 * interval '1 second')
						AND sp.payload->'shown_signatures' @> jsonb_build_array(ev.payload->>'result_signature')
				)
			GROUP BY sig, ev.user_id
		) per_user
		GROUP BY sig
		HAVING SUM(user_score) <> 0`,
		since, shortDwellThresholdMs, perUserSignalCap, int64(shownResultWindow/time.Second),
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

// discographyLatestSQL reduces the discography_observed rows in the window to one
// case per artist — the latest observation, since each open recomputes the whole
// verdict and older rows are stale — then orders those cases worst-first by the
// no-id suspect ratio (single-provider-without-a-shared-id releases over total),
// with the plain single-provider headcount ratio as the fallback tie-break, so the
// LIMIT retains the worst artists rather than the most recent. single_provider_no_id
// is read through the same jsonb_typeof guard as the other fields (an older payload
// without it degrades to 0, so it simply falls back to the headcount ratio); every
// division is guarded by releases > 0 so a zero-release row can never divide by
// zero. by= grouping is applied in Go over this base set, never in SQL, so a
// hostile by= has no path into this query.
const discographyLatestSQL = `SELECT artist_ref, releases, single_provider, single_provider_no_id, provider_counts, occurred_at
	FROM (
		SELECT DISTINCT ON (payload->>'artist_ref')
			COALESCE(payload->>'artist_ref', '') AS artist_ref,
			CASE WHEN jsonb_typeof(payload->'releases') = 'number'
				THEN (payload->>'releases')::int ELSE 0 END AS releases,
			CASE WHEN jsonb_typeof(payload->'single_provider') = 'number'
				THEN (payload->>'single_provider')::int ELSE 0 END AS single_provider,
			CASE WHEN jsonb_typeof(payload->'single_provider_no_id') = 'number'
				THEN (payload->>'single_provider_no_id')::int ELSE 0 END AS single_provider_no_id,
			CASE WHEN jsonb_typeof(payload->'provider_counts') = 'object'
				THEN payload->'provider_counts' ELSE '{}'::jsonb END AS provider_counts,
			occurred_at
		FROM discovery_events
		WHERE event_type = $1
			AND occurred_at >= $2
		ORDER BY payload->>'artist_ref', occurred_at DESC
	) latest
	ORDER BY
		CASE WHEN releases > 0 THEN single_provider_no_id::float8 / releases ELSE 0 END DESC,
		CASE WHEN releases > 0 THEN single_provider::float8 / releases ELSE 0 END DESC,
		releases DESC,
		occurred_at DESC
	LIMIT $3`

// DiscographyQuality reads the discography structural-quality cases inside the
// window, one per artist (latest observation), ordered worst-first. Each row's
// payload is the verdict already computed at the merge in go-api; this is a pure
// read that never recomputes it. groupBy re-clusters the worst-first order by
// artist, provider, or contamination band. A row with a malformed provider_counts
// blob keeps its case but loses its provider split rather than failing the read.
func (r *PgxEventStore) DiscographyQuality(ctx context.Context, since time.Time, groupBy ports.DiscographyGroupBy, limit int) ([]ports.DiscographyCase, error) {
	rows, err := r.pool.Query(ctx, discographyLatestSQL,
		domain.EventTypeDiscographyObserved.String(), since, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("query discography quality: %w", err)
	}
	defer rows.Close()

	cases, err := collectRows(rows, func(rows pgx.Rows) (ports.DiscographyCase, error) {
		var (
			c              ports.DiscographyCase
			providerCounts []byte
		)
		if err := rows.Scan(&c.ArtistRef, &c.Releases, &c.SingleProvider, &c.SingleProviderNoID, &providerCounts, &c.LastSeen); err != nil {
			return ports.DiscographyCase{}, fmt.Errorf("scan discography case: %w", err)
		}
		c.ProviderCounts = map[string]int{}
		if len(providerCounts) > 0 {
			if err := json.Unmarshal(providerCounts, &c.ProviderCounts); err != nil {
				// A corrupt blob loses only its provider split, not the case.
				c.ProviderCounts = map[string]int{}
			}
		}
		return c, nil
	})
	if err != nil {
		return nil, err
	}
	return rankDiscographyCases(cases, groupBy), nil
}

// noIDSuspectRatio is the worst-first primary key: the share of an artist's
// releases that exactly one provider supplied AND that carry no shared id — the
// real contamination suspects, since the id (not the provider headcount) is the
// anchor. An id-verified single-provider release is not counted here, so it never
// ranks as a top suspect. It is 0 (never a divide-by-zero or a negative) for an
// empty, zero-release, or adversarially-negative case.
func noIDSuspectRatio(c ports.DiscographyCase) float64 {
	if c.Releases <= 0 {
		return 0
	}
	noID := c.SingleProviderNoID
	if noID < 0 {
		noID = 0
	}
	return float64(noID) / float64(c.Releases)
}

// contaminationRatio is the fallback key: the share of an artist's releases
// exactly one provider supplied, regardless of id backing. It only breaks ties
// once the id-anchored noIDSuspectRatio is equal — plain headcount is the fallback,
// never the primary signal. It is 0 (never a divide-by-zero or a negative) for an
// empty, zero-release, or adversarially-negative case.
func contaminationRatio(c ports.DiscographyCase) float64 {
	if c.Releases <= 0 {
		return 0
	}
	single := c.SingleProvider
	if single < 0 {
		single = 0
	}
	return float64(single) / float64(c.Releases)
}

// providerImbalance is the tie-break: the spread between the busiest and quietest
// provider's release counts. A lone provider (or none) has no imbalance. Negative
// counts from an adversarial payload are floored at 0 so the spread stays sane.
func providerImbalance(c ports.DiscographyCase) int {
	if len(c.ProviderCounts) < 2 {
		return 0
	}
	first := true
	minN, maxN := 0, 0
	for _, n := range c.ProviderCounts {
		if n < 0 {
			n = 0
		}
		if first {
			minN, maxN, first = n, n, false
			continue
		}
		if n < minN {
			minN = n
		}
		if n > maxN {
			maxN = n
		}
	}
	return maxN - minN
}

// dominantProvider is the provider that supplied the most of an artist's releases,
// ties broken lexicographically for determinism; "" when there are no providers.
// It is the cluster key for by=provider.
func dominantProvider(c ports.DiscographyCase) string {
	best, bestN := "", -1
	for p, n := range c.ProviderCounts {
		if n > bestN || (n == bestN && p < best) {
			best, bestN = p, n
		}
	}
	return best
}

// caseWorseThan is the total worst-first order over cases: higher no-id suspect
// ratio first (the id anchor), then the plain contamination ratio as the headcount
// fallback, then higher provider imbalance, then more releases (a bigger problem),
// then artist_ref ascending so the order is deterministic.
func caseWorseThan(a, b ports.DiscographyCase) bool {
	if na, nb := noIDSuspectRatio(a), noIDSuspectRatio(b); na != nb {
		return na > nb
	}
	ra, rb := contaminationRatio(a), contaminationRatio(b)
	if ra != rb {
		return ra > rb
	}
	ia, ib := providerImbalance(a), providerImbalance(b)
	if ia != ib {
		return ia > ib
	}
	if a.Releases != b.Releases {
		return a.Releases > b.Releases
	}
	return a.ArtistRef < b.ArtistRef
}

// contaminationBand buckets a case by ratio into an ordered band: 0 high, 1
// medium, 2 low. It is the sort key for by=contamination_band (lower band first).
func contaminationBand(c ports.DiscographyCase) int {
	switch r := contaminationRatio(c); {
	case r >= 0.5:
		return 0
	case r >= 0.2:
		return 1
	default:
		return 2
	}
}

// rankDiscographyCases orders the base cases worst-first and re-clusters that
// order by the requested dimension. It is pure and total: an unknown groupBy is
// treated as artist. The input slice is sorted in place (the adapter owns it).
func rankDiscographyCases(cases []ports.DiscographyCase, groupBy ports.DiscographyGroupBy) []ports.DiscographyCase {
	switch groupBy {
	case ports.GroupByProvider:
		return clusterByProvider(cases)
	case ports.GroupByContaminationBand:
		sort.SliceStable(cases, func(i, j int) bool {
			if bi, bj := contaminationBand(cases[i]), contaminationBand(cases[j]); bi != bj {
				return bi < bj
			}
			return caseWorseThan(cases[i], cases[j])
		})
		return cases
	default:
		sort.SliceStable(cases, func(i, j int) bool { return caseWorseThan(cases[i], cases[j]) })
		return cases
	}
}

// clusterByProvider groups the cases by their dominant provider, orders the
// clusters worst-first (by the cluster's aggregate contamination ratio, then its
// total releases, then provider name), and orders artists worst-first within each
// cluster. The returned cases are still per-artist; only their order changes.
func clusterByProvider(cases []ports.DiscographyCase) []ports.DiscographyCase {
	type cluster struct {
		provider         string
		single, releases int
		members          []ports.DiscographyCase
	}
	byProvider := map[string]*cluster{}
	order := []string{}
	for _, c := range cases {
		p := dominantProvider(c)
		cl, ok := byProvider[p]
		if !ok {
			cl = &cluster{provider: p}
			byProvider[p] = cl
			order = append(order, p)
		}
		cl.members = append(cl.members, c)
		if c.Releases > 0 {
			cl.releases += c.Releases
			if c.SingleProvider > 0 {
				cl.single += c.SingleProvider
			}
		}
	}
	clusters := make([]*cluster, 0, len(order))
	for _, p := range order {
		clusters = append(clusters, byProvider[p])
	}
	sort.SliceStable(clusters, func(i, j int) bool {
		ri := clusterRatio(clusters[i].single, clusters[i].releases)
		rj := clusterRatio(clusters[j].single, clusters[j].releases)
		if ri != rj {
			return ri > rj
		}
		if clusters[i].releases != clusters[j].releases {
			return clusters[i].releases > clusters[j].releases
		}
		return clusters[i].provider < clusters[j].provider
	})
	out := make([]ports.DiscographyCase, 0, len(cases))
	for _, cl := range clusters {
		members := cl.members
		sort.SliceStable(members, func(i, j int) bool { return caseWorseThan(members[i], members[j]) })
		out = append(out, members...)
	}
	return out
}

// clusterRatio is a cluster's aggregate contamination ratio, guarded against a
// zero-release cluster.
func clusterRatio(single, releases int) float64 {
	if releases <= 0 {
		return 0
	}
	return float64(single) / float64(releases)
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
