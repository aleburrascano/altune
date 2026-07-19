package app

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"altune/go-api/internal/admin/evalmeter"
	domain "altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

// evalSmokeChecks is a tiny fixed set of canonical queries (the discovery
// spot-checks) the meter runs to catch search-quality regressions. Deliberately
// small: a handful of queries per interval can't meaningfully burn provider
// quota, unlike the full-corpus offline harness (cmd/discoveryeval).
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

// buildEvalRunner returns the live eval-meter runner, or nil when the meter is
// disabled (so no second provider stack is constructed).
func (a *App) buildEvalRunner() evalmeter.Runner {
	if !a.cfg.EvalMeterEnabled {
		return nil
	}
	// A dedicated search-service instance with its OWN per-provider circuit
	// breakers, isolated from production's — eval failures can never trip the
	// breakers live search depends on. Reuses the pool + redis; nil event store
	// so synthetic eval searches don't pollute telemetry. rankingOnly: skips the
	// shared result cache (a cached hit would score the cache, not the pipeline,
	// masking a regression for the TTL) and display enrichment (artwork HTTP the
	// title/subtitle match never reads) while keeping every rank-affecting flag live.
	evalSvc := BuildSearchServiceWithTransport(a.cfg, a.pool, a.redisClient, nil, nil, nil, true)

	evalUser, err := shared.ParseUserId(a.cfg.OperatorUserID)
	if err != nil {
		slog.Warn("eval runner: invalid OperatorUserID, using random", "error", err)
		evalUser = shared.NewUserId(uuid.New())
	}

	return func(ctx context.Context) (evalmeter.Result, error) {
		return runSmokeEval(ctx, evalSvc, evalUser)
	}
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

// matchPosition returns the index of the first result whose title/subtitle
// contains expect, or -1 if none match in the returned list.
func matchPosition(results []domain.SearchResult, expect string) int {
	expect = strings.ToLower(expect)
	for i, r := range results {
		if strings.Contains(strings.ToLower(r.Title+" "+r.Subtitle), expect) {
			return i
		}
	}
	return -1
}
