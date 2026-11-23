package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/adapters/persistence/discographyrank"
	"altune/go-api/internal/discovery/internal/core/ports"
	"context"
	"encoding/json"
	"fmt"
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
	return discographyrank.Rank(cases, groupBy), nil
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
