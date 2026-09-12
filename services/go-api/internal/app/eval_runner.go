package app

import (
	"context"
	"fmt"
	"strings"

	domain "altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
)

// EvalQueryResult is the app-owned outcome of a single smoke-eval query. The
// wiring boundary (app.go) maps it to admin/evalmeter's wire DTO, so this
// package's scoring logic no longer depends on that presentation type.
type EvalQueryResult struct {
	Query    string
	Expect   string
	Passed   bool
	Position int
}

// EvalResult is the app-owned smoke-eval scorecard. app.go maps it to
// admin/evalmeter.Result at the boundary; the JSON shape lives with the meter.
type EvalResult struct {
	Score     float64
	Baseline  float64
	Regressed bool
	Queries   []EvalQueryResult
}

// EvalRunner runs one smoke eval and returns the app-owned result. app.go
// adapts it to admin/evalmeter.Runner when wiring the meter.
type EvalRunner func(ctx context.Context) (EvalResult, error)

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

func (a *App) buildEvalRunner() EvalRunner {
	if !a.cfg.EvalMeterEnabled {
		return nil
	}
	evalSvc := BuildSearchServiceWithTransport(a.cfg, a.pool, a.redisClient, nil, nil, nil, true)

	return func(ctx context.Context) (EvalResult, error) {
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

func runSmokeEval(ctx context.Context, svc *discoveryService.Service, user shared.UserId) (EvalResult, error) {
	kinds := map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}

	passed := 0
	queries := make([]EvalQueryResult, 0, len(evalSmokeChecks))
	for _, check := range evalSmokeChecks {
		query, err := domain.NewSearchQuery(check.query, kinds, evalLimit)
		if err != nil {
			return EvalResult{}, fmt.Errorf("eval query %q: %w", check.query, err)
		}
		out, err := svc.Execute(ctx, user, query, false)
		if err != nil {
			return EvalResult{}, fmt.Errorf("eval search %q: %w", check.query, err)
		}
		pos := matchPosition(out.Results, check.expect)
		ok := pos >= 0 && pos < evalTopK
		if ok {
			passed++
		}
		queries = append(queries, EvalQueryResult{
			Query:    check.query,
			Expect:   check.expect,
			Passed:   ok,
			Position: pos,
		})
	}

	score := float64(passed) / float64(len(evalSmokeChecks))
	return EvalResult{
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
