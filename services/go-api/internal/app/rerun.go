package app

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"altune/go-api/internal/admin/requeststore"
	"altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/textnorm"
)

const rerunBodyCap = 64 * 1024

func reRun(
	ctx context.Context,
	cfg *config.Config,
	transport http.RoundTripper,
	behavioralScores func() map[string]float64,
	query string,
	kinds []string,
) (requeststore.ReRunResult, error) {
	kindSet, err := parseRerunKinds(kinds)
	if err != nil {
		return requeststore.ReRunResult{}, err
	}
	if _, err := domain.NewSearchQuery(query, kindSet, inspectionSearchLimit); err != nil {
		return requeststore.ReRunResult{}, err
	}
	rec := requeststore.NewRerunRecorder(transport, rerunBodyCap)
	provs := BuildDiscoveryProviders(cfg, rec)

	cleaned := discoveryService.CleanQuery(query)
	queryNorm := textnorm.NormalizeForMatch(cleaned)

	start := time.Now()
	perProvider, providerTraces := fanOutRerun(ctx, provs, cleaned, kindSet)
	merged := discoveryService.Merge(perProvider)
	explained := discoveryService.RankExplain(merged, queryNorm, discoveryService.RankOptions{
		TailDemotion:        cfg.TailDemotionEnabled,
		CrossKindProminence: cfg.CrossKindProminenceEnabled,
		Behavioral:          behavioralScores(),
	})
	ranked := make([]domain.SearchResult, len(explained))
	for i, s := range explained {
		ranked[i] = s.Result
	}
	final := discoveryService.Reshape(ranked)

	return requeststore.ReRunResult{
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

func projectScored(explained []discoveryService.ScoredResult) []requeststore.ScoredRow {
	out := make([]requeststore.ScoredRow, len(explained))
	for i, s := range explained {
		rows := requeststore.ProjectResults([]domain.SearchResult{s.Result})
		out[i] = requeststore.ScoredRow{
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
			defer func() {
				if r := recover(); r != nil {
					perProvider[i] = nil
					traces[i] = requeststore.ProviderTrace{
						Provider:  p.Name().String(),
						Status:    domain.ProviderStatusError.String(),
						LatencyMs: time.Since(start).Milliseconds(),
						Err:       fmt.Sprintf("panic: %v", r),
					}
				}
			}()
			results, err := p.Search(ctx, query, kinds)
			perProvider[i] = results
			status := domain.ProviderStatusOK
			errMsg := ""
			if err != nil {
				status = domain.ProviderStatusError
				errMsg = requeststore.RedactSecrets(err.Error())
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

func parseRerunKinds(kinds []string) (map[domain.ResultKind]bool, error) {
	out := map[domain.ResultKind]bool{}
	var invalid []string
	for _, k := range kinds {
		rk, err := domain.ParseResultKind(k)
		if err != nil {
			invalid = append(invalid, k)
			continue
		}
		out[rk] = true
	}
	if len(invalid) > 0 {
		return nil, fmt.Errorf("invalid kinds: %s", strings.Join(invalid, ", "))
	}
	if len(out) == 0 {
		out[domain.ResultKindTrack] = true
		out[domain.ResultKindAlbum] = true
		out[domain.ResultKindArtist] = true
	}
	return out, nil
}

func sortedKindNames(kinds map[domain.ResultKind]bool) []string {
	out := make([]string, 0, len(kinds))
	for k := range kinds {
		out = append(out, k.String())
	}
	sort.Strings(out)
	return out
}
