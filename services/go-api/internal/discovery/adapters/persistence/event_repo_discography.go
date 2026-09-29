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

var discographyLatestSQL = fmt.Sprintf(`SELECT artist_ref, releases, single_provider, single_provider_no_id, provider_counts, occurred_at
	FROM (
		SELECT DISTINCT ON (payload->>'%[1]s')
			COALESCE(payload->>'%[1]s', '') AS artist_ref,
			CASE WHEN jsonb_typeof(payload->'%[2]s') = 'number'
				THEN (payload->>'%[2]s')::int ELSE 0 END AS releases,
			CASE WHEN jsonb_typeof(payload->'%[3]s') = 'number'
				THEN (payload->>'%[3]s')::int ELSE 0 END AS single_provider,
			CASE WHEN jsonb_typeof(payload->'%[4]s') = 'number'
				THEN (payload->>'%[4]s')::int ELSE 0 END AS single_provider_no_id,
			CASE WHEN jsonb_typeof(payload->'%[5]s') = 'object'
				THEN payload->'%[5]s' ELSE '{}'::jsonb END AS provider_counts,
			occurred_at
		FROM discovery_events
		WHERE event_type = $1
			AND occurred_at >= $2
		ORDER BY payload->>'%[1]s', occurred_at DESC
	) latest
	ORDER BY
		CASE WHEN releases > 0 THEN single_provider_no_id::float8 / releases ELSE 0 END DESC,
		CASE WHEN releases > 0 THEN single_provider::float8 / releases ELSE 0 END DESC,
		releases DESC,
		occurred_at DESC
	LIMIT $3`,
	domain.PayloadKeyArtistRef, domain.PayloadKeyReleases, domain.PayloadKeySingleProvider,
	domain.PayloadKeySingleProviderNoId, domain.PayloadKeyProviderCounts)

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

func dominantProvider(c ports.DiscographyCase) string {
	best, bestN := "", -1
	for p, n := range c.ProviderCounts {
		if n > bestN || (n == bestN && p < best) {
			best, bestN = p, n
		}
	}
	return best
}

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

func clusterRatio(single, releases int) float64 {
	if releases <= 0 {
		return 0
	}
	return float64(single) / float64(releases)
}

var discographySuspectRateSQL = fmt.Sprintf(`SELECT
		COUNT(*) AS opens,
		COUNT(*) FILTER (
			WHERE CASE WHEN jsonb_typeof(payload->'%[1]s') = 'number'
				THEN (payload->>'%[1]s')::int > 0 ELSE false END
		) AS suspect_opens,
		MAX(occurred_at) AS last_sample
	FROM discovery_events
	WHERE event_type = $1
		AND occurred_at >= $2`, domain.PayloadKeySingleProviderNoId)

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
