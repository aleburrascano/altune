package app

import (
	"context"
	"fmt"
	"strings"

	"altune/go-api/internal/admin/evalmeter"
	domain "altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
)

var evalSmokeChecks = []struct{ query, expect string }{
	{"Bohemian Rhapsody", "bohemian rhapsody"},
	{"Blinding Lights", "blinding lights"},
	{"Kendrick Lamar Humble", "humble"},
	{"Drake", "drake"},
	{"Bad Bunny", "bad bunny"},
}

const (
	evalBaseline = 0.80
	evalTopK     = 3
	evalLimit    = 10
)

func (a *App) buildEvalRunner() evalmeter.Runner {
	if !a.cfg.EvalMeterEnabled {
		return nil
	}
	evalSvc := BuildSearchServiceWithTransport(a.cfg, a.pool, a.redisClient, nil, nil, nil, true)

	return func(ctx context.Context) (evalmeter.Result, error) {
		return runSmokeEval(ctx, evalSvc, evalUserId())
	}
}

// evalUserId is the identity the smoke eval runs under. It is the synthetic
// system account, never the real operator: the eval exercises the real per-user
// code paths, so running it as a real account would read and persist that
// user's favorites and behavioral signal.
func evalUserId() shared.UserId {
	return shared.SystemUserId()
}

func runSmokeEval(ctx context.Context, svc *discoveryService.Service, user shared.UserId) (evalmeter.Result, error) {
	kinds := map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}

	passed := 0
	queries := make([]evalmeter.QueryResult, 0, len(evalSmokeChecks))
	for _, check := range evalSmokeChecks {
		query, err := domain.NewSearchQuery(check.query, kinds, evalLimit)
		if err != nil {
			return evalmeter.Result{}, fmt.Errorf("eval query %q: %w", check.query, err)
		}
		out, err := svc.Execute(ctx, user, query, false)
		if err != nil {
			return evalmeter.Result{}, fmt.Errorf("eval search %q: %w", check.query, err)
		}
		pos := matchPosition(out.Results, check.expect)
		ok := pos >= 0 && pos < evalTopK
		if ok {
			passed++
		}
		queries = append(queries, evalmeter.QueryResult{
			Query:    check.query,
			Expect:   check.expect,
			Passed:   ok,
			Position: pos,
		})
	}

	score := float64(passed) / float64(len(evalSmokeChecks))
	return evalmeter.Result{
		Score:     score,
		Baseline:  evalBaseline,
		Regressed: score < evalBaseline,
		Queries:   queries,
	}, nil
}

func matchPosition(results []domain.SearchResult, expect string) int {
	expect = strings.ToLower(expect)
	for i, r := range results {
		if strings.Contains(strings.ToLower(r.Title+" "+r.Subtitle), expect) {
			return i
		}
	}
	return -1
}
