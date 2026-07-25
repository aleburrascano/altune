package app

import (
	"context"
	"sort"
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

const rerunBodyCap = 64 * 1024

type reRunner struct {
	cfg              *config.Config
	behavioralScores func() map[string]float64
}

func (a *App) buildReRunner(svc *discoveryService.Service) *reRunner {
	return &reRunner{cfg: a.cfg, behavioralScores: svc.BehavioralScoresSnapshot}
}

func (rr *reRunner) ReRun(ctx context.Context, query string, kinds []string) (adminHandler.ReRunResult, error) {
	kindSet := parseRerunKinds(kinds)
	rec := requeststore.NewExchangeRecorder(defaultLiveTransport, rerunBodyCap)
	provs := BuildDiscoveryProviders(rr.cfg, rec)

	cleaned := discoveryService.CleanQuery(query)
	queryNorm := textnorm.NormalizeForMatch(cleaned)

	start := time.Now()
	perProvider, providerTraces := fanOutRerun(ctx, provs, cleaned, kindSet)
	merged := discoveryService.Merge(perProvider)
	explained := discoveryService.RankExplain(merged, queryNorm, discoveryService.RankOptions{
		TailDemotion:        rr.cfg.TailDemotionEnabled,
		CrossKindProminence: rr.cfg.CrossKindProminenceEnabled,
		Behavioral:          rr.behavioralScores(),
	})
	ranked := make([]domain.SearchResult, len(explained))
	for i, s := range explained {
		ranked[i] = s.Result
	}
	final := discoveryService.Reshape(ranked)

	return adminHandler.ReRunResult{
		Query:     query,
		Kinds:     sortedKindNames(kindSet),
		Providers: providerTraces,
		Exchanges: rec.Exchanges(),
		Merged:    projectEntities(merged),
		RankTrace: projectScored(explained),
		Final:     requeststore.ProjectResults(final),
		TookMs:    time.Since(start).Milliseconds(),
	}, nil
}

func projectScored(explained []discoveryService.ScoredResult) []adminHandler.ScoredRow {
	out := make([]adminHandler.ScoredRow, len(explained))
	for i, s := range explained {
		rows := requeststore.ProjectResults([]domain.SearchResult{s.Result})
		out[i] = adminHandler.ScoredRow{
			ResultRow:   rows[0],
			Relevance:   s.Relevance,
			Prominence:  s.Prominence,
			Behavioral:  s.Behavioral,
			Popularity:  s.Popularity,
			RRF:         s.RRF,
			MultiSource: s.MultiSource,
			Demoted:     s.Demoted,
		}
	}
	return out
}

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
			errMsg := ""
			if err != nil {
				status = domain.ProviderStatusError
				errMsg = err.Error()
			}
			traces[i] = requeststore.ProviderTrace{
				Provider:    p.Name().String(),
				Status:      status.String(),
				LatencyMs:   time.Since(start).Milliseconds(),
				ResultCount: len(results),
				Err:         errMsg,
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
	sort.Strings(out)
	return out
}
