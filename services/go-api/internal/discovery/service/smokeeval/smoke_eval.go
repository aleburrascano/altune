package smokeeval

import (
	domain "altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/service/search"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
)

type QueryResult struct {
	Query    string
	Expect   string
	Passed   bool
	Position int
}

type Result struct {
	Score     float64
	Baseline  float64
	Regressed bool
	Errored   int
	Queries   []QueryResult
}

type searcher interface {
	Execute(
		ctx context.Context,
		userId shared.UserId,
		query *domain.SearchQuery,
		saveHistory bool,
	) (*search.SearchOutput, error)
}

var smokeChecks = []struct{ query, expect string }{
	{"Bohemian Rhapsody", "bohemian rhapsody"},
	{"Blinding Lights", "blinding lights"},
	{"Kendrick Lamar Humble", "humble"},
	{"Drake", "drake"},
	{"Bad Bunny", "bad bunny"},
}

const (
	baseline = 0.80
	topK     = 3
	limit    = 10
)

func Run(ctx context.Context, svc searcher, user shared.UserId) (Result, error) {
	queries, passed, errored := runChecks(ctx, svc, user)
	if errored == len(smokeChecks) {
		return Result{}, fmt.Errorf("%w (%d queries)", errEveryQueryErrored, errored)
	}

	score := float64(passed) / float64(len(smokeChecks))
	return Result{
		Score:     score,
		Baseline:  baseline,
		Regressed: isRankingRegression(score, errored),
		Errored:   errored,
		Queries:   queries,
	}, nil
}

func runChecks(ctx context.Context, svc searcher, user shared.UserId) (queries []QueryResult, passed, errored int) {
	kinds := map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
	queries = make([]QueryResult, 0, len(smokeChecks))
	for _, check := range smokeChecks {
		res, err := evalQuery(ctx, svc, user, check.query, check.expect, kinds)
		if err != nil {
			errored++
			slog.WarnContext(ctx, "eval.smoke.query_errored", "query", check.query, "error", err)
			res = QueryResult{Query: check.query, Expect: check.expect, Position: -1}
		} else if res.Passed {
			passed++
		}
		queries = append(queries, res)
	}
	return queries, passed, errored
}

var errEveryQueryErrored = errors.New("every smoke-eval query errored")

func isRankingRegression(score float64, errored int) bool {
	if errored > 0 {
		return false
	}
	return score < baseline
}

func evalQuery(
	ctx context.Context,
	svc searcher,
	user shared.UserId,
	q, expect string,
	kinds map[domain.ResultKind]bool,
) (QueryResult, error) {
	query, err := domain.NewSearchQuery(q, kinds, limit)
	if err != nil {
		return QueryResult{}, fmt.Errorf("eval query %q: %w", q, err)
	}
	out, err := svc.Execute(ctx, user, query, false)
	if err != nil {
		return QueryResult{}, fmt.Errorf("eval search %q: %w", q, err)
	}
	pos := matchPosition(out.Results, expect)
	return QueryResult{Query: q, Expect: expect, Passed: pos >= 0 && pos < topK, Position: pos}, nil
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
