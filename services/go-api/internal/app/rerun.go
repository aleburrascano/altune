package app

import (
	"context"
	"sync"
	"time"

	adminHandler "altune/go-api/internal/admin/handler"
	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/textnorm"
)

// rerunBodyCap bounds each captured raw provider body in a re-run response so the
// operator console payload stays sane even for chatty providers.
const rerunBodyCap = 64 * 1024

// reRunner runs an operator-supplied query live through the real discovery
// decision core (the same exported Merge → RankWith → Reshape composition the
// search path uses, including the flag-gated experiment stages), over a
// recording HTTP client, and returns the full stage-by-stage waterfall. It
// builds providers directly — no Service — so it bypasses the live circuit
// breakers by construction (a re-run can't trip a breaker live users depend on).
type reRunner struct {
	cfg *config.Config
	// behavioralScores reads the live Service's published satisfaction snapshot,
	// so a re-run ranks with the same behavioral input production applies. Reading
	// the snapshot touches no breaker.
	behavioralScores func() map[string]float64
}

func (a *App) buildReRunner(svc *discoveryService.Service) *reRunner {
	return &reRunner{cfg: a.cfg, behavioralScores: svc.BehavioralScoresSnapshot}
}

// ReRun satisfies adminHandler.ReRunner.
func (rr *reRunner) ReRun(ctx context.Context, query string, kinds []string) (adminHandler.ReRunResult, error) {
	kindSet := parseRerunKinds(kinds)
	rec := requeststore.NewExchangeRecorder(defaultLiveTransport, rerunBodyCap)
	provs := BuildDiscoveryProviders(rr.cfg, rec)

	cleaned := discoveryService.CleanQuery(query)
	queryNorm := textnorm.NormalizeForMatch(cleaned)

	start := time.Now()
	perProvider, providerTraces := fanOutRerun(ctx, provs, cleaned, kindSet)
	merged := discoveryService.Merge(perProvider)
	// Rank with the same flag-gated experiment stages production applies — a
	// zero-value config here once made the waterfall silently diverge from live
	// ranking whenever a flag was on (cross-kind prominence defaults on).
	ranked := discoveryService.RankWith(merged, queryNorm, discoveryService.RankOptions{
		TailDemotion:        rr.cfg.TailDemotionEnabled,
		CrossKindProminence: rr.cfg.CrossKindProminenceEnabled,
		Behavioral:          rr.behavioralScores(),
	})
	final := discoveryService.Reshape(ranked)

	return adminHandler.ReRunResult{
		Query:     query,
		Kinds:     sortedKindNames(kindSet),
		Providers: providerTraces,
		Exchanges: rec.Exchanges(),
		Merged:    projectEntities(merged),
		Ranked:    requeststore.ProjectResults(ranked),
		Final:     requeststore.ProjectResults(final),
		TookMs:    time.Since(start).Milliseconds(),
	}, nil
}

// fanOutRerun queries every provider concurrently (no breaker), returning the raw
// per-provider groups for merge plus a display projection per provider.
func fanOutRerun(
	ctx context.Context,
	provs []discoveryPorts.SearchProvider,
	query string,
	kinds map[domain.ResultKind]bool,
) ([][]domain.SearchResult, []requeststore.ProviderTrace) {
	perProvider := make([][]domain.SearchResult, len(provs))
	traces := make([]requeststore.ProviderTrace, len(provs))
	var wg sync.WaitGroup
	for i, p := range provs {
		wg.Add(1)
		go func(i int, p discoveryPorts.SearchProvider) {
			defer wg.Done()
			start := time.Now()
			results, err := p.Search(ctx, query, kinds)
			perProvider[i] = results
			status := domain.ProviderStatusOK
			if err != nil {
				status = domain.ProviderStatusError
			}
			traces[i] = requeststore.ProviderTrace{
				Provider:    p.Name().String(),
				Status:      status.String(),
				LatencyMs:   time.Since(start).Milliseconds(),
				ResultCount: len(results),
				Results:     requeststore.ProjectResults(results),
			}
		}(i, p)
	}
	wg.Wait()
	return perProvider, traces
}

func projectEntities(entities []discoveryService.Entity) []requeststore.ResultRow {
	results := make([]domain.SearchResult, 0, len(entities))
	for _, e := range entities {
		results = append(results, e.Result)
	}
	return requeststore.ProjectResults(results)
}

func parseRerunKinds(kinds []string) map[domain.ResultKind]bool {
	out := map[domain.ResultKind]bool{}
	for _, k := range kinds {
		if rk, err := domain.ParseResultKind(k); err == nil {
			out[rk] = true
		}
	}
	if len(out) == 0 {
		out[domain.ResultKindTrack] = true
		out[domain.ResultKindAlbum] = true
		out[domain.ResultKindArtist] = true
	}
	return out
}

func sortedKindNames(kinds map[domain.ResultKind]bool) []string {
	out := make([]string, 0, len(kinds))
	for k := range kinds {
		out = append(out, k.String())
	}
	return out
}
