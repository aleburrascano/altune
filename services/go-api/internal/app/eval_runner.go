package app

import (
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	domain "altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
)

type EvalQueryResult struct {
	Query    string
	Expect   string
	Passed   bool
	Position int
}

type EvalResult struct {
	Score     float64
	Baseline  float64
	Regressed bool
	Errored   int
	Queries   []EvalQueryResult
}

type evalSearcher interface {
	Execute(
		ctx context.Context,
		userId shared.UserId,
		query *domain.SearchQuery,
		saveHistory bool,
	) (*discoveryService.SearchOutput, error)
}

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
	evalSvc := BuildRankingOnlySearchService(a.cfg, a.pool, a.redisClient, nil)

	return func(ctx context.Context) (EvalResult, error) {
		return runSmokeEval(ctx, evalSvc, evalUserId())
	}
}

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
			errored++
			slog.WarnContext(ctx, "eval.smoke.query_errored", "query", check.query, "error", err)
			res = EvalQueryResult{Query: check.query, Expect: check.expect, Position: -1}
		} else if res.Passed {
			passed++
		}
		queries = append(queries, res)
	}

	if errored == len(evalSmokeChecks) {
		return EvalResult{}, fmt.Errorf("%w (%d queries)", errEveryEvalQueryErrored, errored)
	}

	score := float64(passed) / float64(len(evalSmokeChecks))
	return EvalResult{
		Score:     score,
		Baseline:  evalBaseline,
		Regressed: isRankingRegression(score, errored),
		Errored:   errored,
		Queries:   queries,
	}, nil
}

var errEveryEvalQueryErrored = errors.New("every smoke-eval query errored")

func isRankingRegression(score float64, errored int) bool {
	if errored > 0 {
		return false
	}
	return score < evalBaseline
}

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
