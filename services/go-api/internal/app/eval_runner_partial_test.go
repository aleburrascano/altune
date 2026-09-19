package app

import (
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"

	domain "altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
)

// scriptedSearcher errors on the configured queries and returns a single
// matching result for every other, so matchPosition scores them as passes.
type scriptedSearcher struct{ failing map[string]bool }

func searcherFailing(queries ...string) scriptedSearcher {
	failing := make(map[string]bool, len(queries))
	for _, q := range queries {
		failing[q] = true
	}
	return scriptedSearcher{failing: failing}
}

func searcherFailingEveryQuery() scriptedSearcher {
	queries := make([]string, len(evalSmokeChecks))
	for i, check := range evalSmokeChecks {
		queries[i] = check.query
	}
	return searcherFailing(queries...)
}

func (s scriptedSearcher) Execute(
	_ context.Context,
	_ shared.UserId,
	query *domain.SearchQuery,
	_ bool,
) (*discoveryService.SearchOutput, error) {
	if s.failing[query.Raw] {
		return nil, errors.New("transient upstream failure")
	}
	// The smoke-eval expectation is a substring of its query, so echoing the
	// query back as a result title lands a top-K match for the non-failing ones.
	return &discoveryService.SearchOutput{
		Results: []domain.SearchResult{{Title: query.Raw}},
	}, nil
}

// A single erroring query must not discard the other queries' results. Before
// the fix runSmokeEval returned early on the first error, collapsing the whole
// scorecard to a fatal error and zero data.
func TestSmokeEval_OneErroringQueryDoesNotDiscardOthers(t *testing.T) {
	const failing = "Drake"
	svc := searcherFailing(failing)

	res, err := runSmokeEval(context.Background(), svc, evalUserId())
	if err != nil {
		t.Fatalf("one erroring query made the whole eval fatal: %v", err)
	}

	if len(res.Queries) != len(evalSmokeChecks) {
		t.Fatalf("recorded %d query results, want all %d", len(res.Queries), len(evalSmokeChecks))
	}
	if res.Errored != 1 {
		t.Errorf("Errored = %d, want 1", res.Errored)
	}

	passed := 0
	for _, q := range res.Queries {
		if q.Passed {
			passed++
		}
		if q.Query == failing && q.Passed {
			t.Error("the erroring query must be recorded as a failed check, not a pass")
		}
	}

	wantPassed := len(evalSmokeChecks) - 1
	if passed != wantPassed {
		t.Errorf("passed = %d, want %d (every non-erroring query still scored)", passed, wantPassed)
	}

	wantScore := float64(wantPassed) / float64(len(evalSmokeChecks))
	if res.Score != wantScore {
		t.Errorf("Score = %v, want partial %v (not collapsed to 0)", res.Score, wantScore)
	}
}

// An outage that takes every query down measured no ranking at all. Before the
// fix it returned a nil error with score 0, which the meter reported as
// StateRegression — every query looking like a ranking failure.
func TestSmokeEval_EveryQueryErroringIsAFailedRunNotAZeroScore(t *testing.T) {
	svc := searcherFailingEveryQuery()

	res, err := runSmokeEval(context.Background(), svc, evalUserId())

	if !errors.Is(err, errEveryEvalQueryErrored) {
		t.Fatalf("err = %v, want errEveryEvalQueryErrored so the meter reports an error state", err)
	}
	if res.Regressed {
		t.Error("a run that scored no query must not claim a ranking regression")
	}
}

// A partial outage scores its errored queries as failed checks, so the score
// alone drops below the baseline. Before the fix that drop was reported as a
// ranking regression.
func TestSmokeEval_PartialOutageIsNotReportedAsARegression(t *testing.T) {
	svc := searcherFailing("Drake", "Bad Bunny")

	res, err := runSmokeEval(context.Background(), svc, evalUserId())
	if err != nil {
		t.Fatalf("a partial outage must still return a scorecard: %v", err)
	}

	if res.Score >= res.Baseline {
		t.Fatalf("Score = %v, want below baseline %v so the case can expose a false regression", res.Score, res.Baseline)
	}
	if res.Regressed {
		t.Errorf("Regressed = true with %d errored queries; an outage is not a ranking regression", res.Errored)
	}
	if res.Errored != 2 {
		t.Errorf("Errored = %d, want 2 so operators can read the outage off the status", res.Errored)
	}
}
