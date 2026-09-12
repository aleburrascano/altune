package app

import (
	"context"
	"fmt"
	"log/slog"
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
//
// Errored counts queries that failed to construct or search. Such a query is
// scored as a failed check (it cannot match), but the count is surfaced
// separately so a transient partial outage ("N queries errored") is
// distinguishable from every query genuinely failing to rank well.
type EvalResult struct {
	Score     float64
	Baseline  float64
	Regressed bool
	Errored   int
	Queries   []EvalQueryResult
}

// evalSearcher is the narrow slice of the discovery service the smoke eval
// needs. Depending on the behaviour rather than the concrete *Service keeps the
// scoring loop unit-testable with an error-injecting fake.
type evalSearcher interface {
	Execute(
		ctx context.Context,
		userId shared.UserId,
		query *domain.SearchQuery,
		saveHistory bool,
	) (*discoveryService.SearchOutput, error)
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

func runSmokeEval(ctx context.Context, svc evalSearcher, user shared.UserId) (EvalResult, error) {
	kinds := map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}

	passed, errored := 0, 0
	queries := make([]EvalQueryResult, 0, len(evalSmokeChecks))
	for _, check := range evalSmokeChecks {
		res, err := evalQuery(ctx, svc, user, check.query, check.expect, kinds)
		if err != nil {
			// A per-query failure is scored as a failed check and the eval
			// continues, so one transient error no longer discards the rest.
			errored++
			slog.WarnContext(ctx, "eval.smoke.query_errored", "query", check.query, "error", err)
			res = EvalQueryResult{Query: check.query, Expect: check.expect, Position: -1}
		} else if res.Passed {
			passed++
		}
		queries = append(queries, res)
	}

	score := float64(passed) / float64(len(evalSmokeChecks))
	return EvalResult{
		Score:     score,
		Baseline:  evalBaseline,
		Regressed: score < evalBaseline,
		Errored:   errored,
		Queries:   queries,
	}, nil
}

// evalQuery runs a single smoke-eval check. Construction and search failures are
// returned as errors for the caller to classify; a clean run yields the scored
// per-query result.
func evalQuery(
	ctx context.Context,
	svc evalSearcher,
	user shared.UserId,
	q, expect string,
	kinds map[domain.ResultKind]bool,
) (EvalQueryResult, error) {
	query, err := domain.NewSearchQuery(q, kinds, evalLimit)
	if err != nil {
		return EvalQueryResult{}, fmt.Errorf("eval query %q: %w", q, err)
	}
	out, err := svc.Execute(ctx, user, query, false)
	if err != nil {
		return EvalQueryResult{}, fmt.Errorf("eval search %q: %w", q, err)
	}
	pos := matchPosition(out.Results, expect)
	return EvalQueryResult{Query: q, Expect: expect, Passed: pos >= 0 && pos < evalTopK, Position: pos}, nil
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
