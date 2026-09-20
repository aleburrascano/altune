package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/jackc/pgx/v5"
)

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

// discographySuspectRateSQL counts, over the discography_observed rows in the
// window, how many opens fired the top release-suspect — an open where
// single_provider_no_id (the id-anchored suspect from #1800) is > 0 — against the
// total opens, and reports the most recent open's occurred_at. It reads only
// discography_observed, which is server-emitted on the live discography path;
// eval/synthetic traffic emits none, so the rate is over real production opens by
// construction. single_provider_no_id is read through the same jsonb_typeof guard
// the ranking uses, so an older payload without it degrades to 0 (that open simply
// does not count as a suspect). The aggregate always returns one row: opens = 0 and
// a NULL last_sample when the window is empty, which the caller renders as a 0 rate.
const discographySuspectRateSQL = `SELECT
		COUNT(*) AS opens,
		COUNT(*) FILTER (
			WHERE CASE WHEN jsonb_typeof(payload->'single_provider_no_id') = 'number'
				THEN (payload->>'single_provider_no_id')::int > 0 ELSE false END
		) AS suspect_opens,
		MAX(occurred_at) AS last_sample
	FROM discovery_events
	WHERE event_type = $1
		AND occurred_at >= $2`

// SuspectRate computes the windowed headline: the share of real discography opens
// whose top release-suspect fired. It is a pure read over the server-emitted
// discography_observed events — the verdict per open was computed at the merge, so
// this only counts opens, it never recomputes disagreement. The rate is guarded
// against an empty window (0 opens yields a 0 rate, never a divide-by-zero).
func (r *PgxEventStore) SuspectRate(ctx context.Context, since time.Time) (ports.DiscographySuspectRate, error) {
	var (
		opens, suspectOpens int
		lastSample          *time.Time
	)
	err := r.pool.QueryRow(ctx, discographySuspectRateSQL,
		domain.EventTypeDiscographyObserved.String(), since,
	).Scan(&opens, &suspectOpens, &lastSample)
	if err != nil {
		return ports.DiscographySuspectRate{}, fmt.Errorf("query discography suspect rate: %w", err)
	}
	out := ports.DiscographySuspectRate{}
	if opens > 0 {
		out.Rate = float64(suspectOpens) / float64(opens)
	}
	if lastSample != nil {
		out.LastSample = lastSample.UTC()
	}
	return out, nil
}
