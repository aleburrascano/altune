package app

import (
	"context"
	"fmt"
	"strings"

	adminHandler "altune/go-api/internal/admin/handler"
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
func (a *App) buildEvalRunner() adminHandler.EvalRunner {
	if !a.cfg.EvalMeterEnabled {
		return nil
	}
	// A dedicated search-service instance with its OWN per-provider circuit
	// breakers, isolated from production's — eval failures can never trip the
	// breakers live search depends on. Reuses the pool + redis; nil event store
	// so synthetic eval searches don't pollute telemetry.
	evalSvc := BuildSearchService(a.cfg, a.pool, a.redisClient, nil)

	evalUser, err := shared.ParseUserId(a.cfg.OperatorUserID)
	if err != nil {
		evalUser = shared.NewUserId(uuid.New())
	}

	return func(ctx context.Context) (adminHandler.EvalResult, error) {
		return runSmokeEval(ctx, evalSvc, evalUser)
	}
}

func runSmokeEval(ctx context.Context, svc *discoveryService.Service, user shared.UserId) (adminHandler.EvalResult, error) {
	kinds := map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}

	passed := 0
	queries := make([]adminHandler.EvalQueryResult, 0, len(evalSmokeChecks))
	for _, check := range evalSmokeChecks {
		query, err := domain.NewSearchQuery(check.query, kinds, evalLimit)
		if err != nil {
			return adminHandler.EvalResult{}, fmt.Errorf("eval query %q: %w", check.query, err)
		}
		out, err := svc.Execute(ctx, user, query, false)
		if err != nil {
			return adminHandler.EvalResult{}, fmt.Errorf("eval search %q: %w", check.query, err)
		}
		pos := matchPosition(out.Results, check.expect)
		ok := pos >= 0 && pos < evalTopK
		if ok {
			passed++
		}
		queries = append(queries, adminHandler.EvalQueryResult{
			Query:    check.query,
			Expect:   check.expect,
			Passed:   ok,
			Position: pos,
		})
	}

	score := float64(passed) / float64(len(evalSmokeChecks))
	return adminHandler.EvalResult{
		Score:     score,
		Baseline:  evalBaseline,
		Regressed: score < evalBaseline,
		Queries:   queries,
	}, nil
}

// topKContains reports whether the expected result lands within the top k.
func topKContains(results []domain.SearchResult, expect string, k int) bool {
	pos := matchPosition(results, expect)
	return pos >= 0 && pos < k
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
