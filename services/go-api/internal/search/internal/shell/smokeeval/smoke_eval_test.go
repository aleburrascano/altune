package smokeeval_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type answering []search.SearchResult

func (a answering) Execute(context.Context, ids.UserId, *search.SearchQuery) (*search.SearchOutput, error) {
	return &search.SearchOutput{Results: a}, nil
}

func listedChecks(t *testing.T) []search.SmokeQueryResult {
	t.Helper()
	res, err := search.RunSmokeEval(context.Background(), answering(nil), ids.SystemUserId())
	if err != nil || len(res.Queries) == 0 {
		t.Fatalf("listing the smoke checks: %+v, %v", res, err)
	}
	return res.Queries
}

func positionFor(t *testing.T, query string, results []search.SearchResult) int {
	t.Helper()
	res, err := search.RunSmokeEval(context.Background(), answering(results), ids.SystemUserId())
	if err != nil {
		t.Fatalf("RunSmokeEval: %v", err)
	}
	for _, q := range res.Queries {
		if q.Query == query {
			return q.Position
		}
	}
	t.Fatalf("no result recorded for %q", query)
	return 0
}

func TestRunSmokeEval_ReportsWhereEachCheckFoundItsExpectedResult(t *testing.T) {
	check := listedChecks(t)[0]
	other := memory.NewResult(0, "Some Other Track", memory.WithSubtitle("Artist"))
	tests := []struct {
		name    string
		results []search.SearchResult
		want    int
	}{
		{"hit at position 0 (case-insensitive)", []search.SearchResult{memory.NewResult(0, strings.ToUpper(check.Expect))}, 0},
		{"hit via subtitle", []search.SearchResult{memory.NewResult(0, "x", memory.WithSubtitle(check.Expect))}, 0},
		{"later match reports its position", []search.SearchResult{other, other, other, memory.NewResult(0, check.Expect)}, 3},
		{"miss", []search.SearchResult{other}, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := positionFor(t, check.Query, tt.results); got != tt.want {
				t.Errorf("position = %d, want %d", got, tt.want)
			}
		})
	}
}

type recordingEventStore struct {
	mu     sync.Mutex
	events []search.InteractionEvent
}

func (r *recordingEventStore) Append(_ context.Context, e search.InteractionEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	return nil
}

func (r *recordingEventStore) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

type stubSearchProvider struct{}

func (stubSearchProvider) Name() search.ProviderName { return search.ProviderDeezer }

func (stubSearchProvider) Search(_ context.Context, _ string, _ map[search.ResultKind]bool) ([]search.SearchResult, error) {
	return nil, nil
}

func (stubSearchProvider) SupportedKinds() map[search.ResultKind]bool {
	return map[search.ResultKind]bool{
		search.ResultKindTrack:  true,
		search.ResultKindAlbum:  true,
		search.ResultKindArtist: true,
	}
}

func runSmokeEvalRecording(t *testing.T, user ids.UserId) int {
	t.Helper()
	bg := searchtest.NewTracker(t)
	store := &recordingEventStore{}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{stubSearchProvider{}}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Events = store })
	if _, err := search.RunSmokeEval(context.Background(), svc, user); err != nil {
		t.Fatalf("Run: %v", err)
	}
	bg.Wait()
	return store.count()
}

func TestSmokeEval_RunsUnderSyntheticIdentity(t *testing.T) {
	if !ids.SystemUserId().IsSystem() {
		t.Fatal("smoke eval must run under the synthetic system identity")
	}

	if n := runSmokeEvalRecording(t, ids.NewUserId(uuid.New())); n == 0 {
		t.Fatal("real identity should persist InteractionEvents (control)")
	}

	if n := runSmokeEvalRecording(t, ids.SystemUserId()); n != 0 {
		t.Fatalf("smoke eval persisted %d InteractionEvents under its identity; want 0", n)
	}
}

type scriptedSearcher struct{ failing map[string]bool }

func searcherFailing(queries ...string) scriptedSearcher {
	failing := make(map[string]bool, len(queries))
	for _, q := range queries {
		failing[q] = true
	}
	return scriptedSearcher{failing: failing}
}

func searcherFailingEveryQuery(t *testing.T) scriptedSearcher {
	t.Helper()
	checks := listedChecks(t)
	queries := make([]string, len(checks))
	for i, q := range checks {
		queries[i] = q.Query
	}
	return searcherFailing(queries...)
}

func (s scriptedSearcher) Execute(
	_ context.Context,
	_ ids.UserId,
	query *search.SearchQuery,
) (*search.SearchOutput, error) {
	if s.failing[query.Raw] {
		return nil, errors.New("transient upstream failure")
	}
	return &search.SearchOutput{
		Results: []search.SearchResult{memory.NewResult(0, query.Raw)},
	}, nil
}

func TestSmokeEval_OneErroringQueryDoesNotDiscardOthers(t *testing.T) {
	const failing = "Drake"
	svc := searcherFailing(failing)

	res, err := search.RunSmokeEval(context.Background(), svc, ids.SystemUserId())
	if err != nil {
		t.Fatalf("one erroring query made the whole eval fatal: %v", err)
	}

	checks := len(listedChecks(t))
	if len(res.Queries) != checks {
		t.Fatalf("recorded %d query results, want all %d", len(res.Queries), checks)
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

	wantPassed := checks - 1
	if passed != wantPassed {
		t.Errorf("passed = %d, want %d (every non-erroring query still scored)", passed, wantPassed)
	}

	wantScore := float64(wantPassed) / float64(checks)
	if res.Score != wantScore {
		t.Errorf("Score = %v, want partial %v (not collapsed to 0)", res.Score, wantScore)
	}
}

func TestSmokeEval_EveryQueryErroringIsAFailedRunNotAZeroScore(t *testing.T) {
	svc := searcherFailingEveryQuery(t)

	res, err := search.RunSmokeEval(context.Background(), svc, ids.SystemUserId())

	if err == nil || !strings.Contains(err.Error(), "every smoke-eval query errored") {
		t.Fatalf("err = %v, want the every-query-errored failure so the meter reports an error state", err)
	}
	if res.Regressed {
		t.Error("a run that scored no query must not claim a ranking regression")
	}
}

func TestSmokeEval_PartialOutageIsNotReportedAsARegression(t *testing.T) {
	svc := searcherFailing("Drake", "Bad Bunny")

	res, err := search.RunSmokeEval(context.Background(), svc, ids.SystemUserId())
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
