package app

import (
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"

	domain "altune/go-api/internal/discovery/domain"
	discoveryService "altune/go-api/internal/discovery/service"
)

// scriptedSearcher errors on one configured query and returns a single
// matching result for every other, so matchPosition scores them as passes.
type scriptedSearcher struct{ failQuery string }

func (s scriptedSearcher) Execute(
	_ context.Context,
	_ shared.UserId,
	query *domain.SearchQuery,
	_ bool,
) (*discoveryService.SearchOutput, error) {
	if query.Raw == s.failQuery {
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
	svc := scriptedSearcher{failQuery: failing}

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
