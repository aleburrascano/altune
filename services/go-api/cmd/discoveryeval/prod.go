package main

import (
	discoveryPersistence "altune/go-api/internal/discovery/adapters/persistence"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

var searchStageKeys = []string{"fanout_ms", "identity_ms", "rank_ms", "artwork_ms", "favorites_ms", "related_ms", "total_ms"}

const abandonedScanLimit = 100000

type searchEvent struct {
	QueryNorm string
	Payload   map[string]any
}

type stageLatency struct {
	Name     string
	P50, P95 int64
}

type prodReport struct {
	Searches, Timed, Fresh int
	Stages                 []stageLatency
	CachedShare            float64
	PartialShare           float64
	ArtistNamed            int
	ArtistFirst            int
	Abandoned              int
}

func runProd(ctx context.Context, pool *pgxpool.Pool, opts options) error {
	since := time.Now().UTC().AddDate(0, 0, -opts.sinceDays)
	events, err := loadSearchEvents(ctx, pool, since)
	if err != nil {
		return err
	}
	abandoned, err := discoveryPersistence.NewPgxEventStore(pool).AbandonedSearches(ctx, since, abandonedScanLimit)
	if err != nil {
		return fmt.Errorf("abandoned searches: %w", err)
	}
	total := 0
	for _, qc := range abandoned {
		total += qc.Count
	}
	fmt.Print(renderProd(summarizeSearches(events, total), opts.sinceDays))
	return nil
}

func loadSearchEvents(ctx context.Context, pool *pgxpool.Pool, since time.Time) ([]searchEvent, error) {
	rows, err := pool.Query(ctx, `SELECT COALESCE(query_norm, ''), payload FROM discovery_events
		WHERE event_type = $1 AND occurred_at >= $2`, domain.EventTypeSearchPerformed.String(), since)
	if err != nil {
		return nil, fmt.Errorf("load search events: %w", err)
	}
	defer rows.Close()
	var events []searchEvent
	for rows.Next() {
		var e searchEvent
		var raw []byte
		if err := rows.Scan(&e.QueryNorm, &raw); err != nil {
			return nil, fmt.Errorf("scan search event: %w", err)
		}
		_ = json.Unmarshal(raw, &e.Payload)
		events = append(events, e)
	}
	return events, rows.Err()
}

func summarizeSearches(events []searchEvent, abandoned int) prodReport {
	r := prodReport{Searches: len(events), Abandoned: abandoned}
	var fresh []searchEvent
	cached, partial := 0, 0
	for _, e := range events {
		r.countArtistIntent(e)
		if _, timed := e.Payload["total_ms"]; !timed {
			continue
		}
		r.Timed++
		if e.Payload["partial"] == true {
			partial++
		}
		if e.Payload["cached"] == true {
			cached++
			continue
		}
		fresh = append(fresh, e)
	}
	r.Fresh = len(fresh)
	r.Stages = stageLatencies(fresh)
	r.CachedShare, r.PartialShare = share(cached, r.Timed), share(partial, r.Timed)
	return r
}

func (r *prodReport) countArtistIntent(e searchEvent) {
	top, _ := e.Payload["top"].([]any)
	for _, raw := range top {
		item, _ := raw.(map[string]any)
		title, _ := item["title"].(string)
		if item["kind"] != "artist" || textnorm.NormalizeForMatch(title) != e.QueryNorm {
			continue
		}
		r.ArtistNamed++
		if item["position"] == 0.0 {
			r.ArtistFirst++
		}
		return
	}
}

func stageLatencies(events []searchEvent) []stageLatency {
	out := make([]stageLatency, 0, len(searchStageKeys))
	for _, key := range searchStageKeys {
		ms := make([]int64, 0, len(events))
		for _, e := range events {
			if v, ok := e.Payload[key].(float64); ok {
				ms = append(ms, int64(v))
			}
		}
		slices.Sort(ms)
		out = append(out, stageLatency{Name: key, P50: nearestRank(ms, 0.50), P95: nearestRank(ms, 0.95)})
	}
	return out
}

func nearestRank(sorted []int64, q float64) int64 {
	if len(sorted) == 0 {
		return 0
	}
	return sorted[int(math.Ceil(q*float64(len(sorted))))-1]
}

func share(n, of int) float64 {
	if of == 0 {
		return 0
	}
	return float64(n) / float64(of)
}

func renderProd(r prodReport, sinceDays int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "\n# prod searches, last %d days\n\n", sinceDays)
	fmt.Fprintf(&b, "  searches: %d  (with stage times: %d, fresh: %d)\n", r.Searches, r.Timed, r.Fresh)
	fmt.Fprintf(&b, "  cached: %.0f%%  partial: %.0f%%\n", r.CachedShare*100, r.PartialShare*100)
	fmt.Fprintf(&b, "  artist first: %d of %d searches that named an artist\n", r.ArtistFirst, r.ArtistNamed)
	fmt.Fprintf(&b, "  abandoned: %d  (no click, same user searched again within 60s)\n\n", r.Abandoned)
	fmt.Fprintf(&b, "  fresh searches   %8s %8s\n", "p50", "p95")
	for _, s := range r.Stages {
		fmt.Fprintf(&b, "  %-16s %6dms %6dms\n", s.Name, s.P50, s.P95)
	}
	return b.String()
}
