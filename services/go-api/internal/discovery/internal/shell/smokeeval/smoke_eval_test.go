package smokeeval_test

import (
	"altune/go-api/internal/discovery"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type answering []discovery.SearchResult

func (a answering) Execute(context.Context, shared.UserId, *discovery.SearchQuery, bool) (*discovery.SearchOutput, error) {
	return &discovery.SearchOutput{Results: a}, nil
}

func listedChecks(t *testing.T) []discovery.SmokeQueryResult {
	t.Helper()
	res, err := discovery.RunSmokeEval(context.Background(), answering(nil), shared.SystemUserId())
	if err != nil || len(res.Queries) == 0 {
		t.Fatalf("listing the smoke checks: %+v, %v", res, err)
	}
	return res.Queries
}

func positionFor(t *testing.T, query string, results []discovery.SearchResult) int {
	t.Helper()
	res, err := discovery.RunSmokeEval(context.Background(), answering(results), shared.SystemUserId())
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
	other := discovery.SearchResult{Title: "Some Other Track", Subtitle: "Artist"}
	tests := []struct {
		name    string
		results []discovery.SearchResult
		want    int
	}{
		{"hit at position 0 (case-insensitive)", []discovery.SearchResult{{Title: strings.ToUpper(check.Expect)}}, 0},
		{"hit via subtitle", []discovery.SearchResult{{Title: "x", Subtitle: check.Expect}}, 0},
		{"later match reports its position", []discovery.SearchResult{other, other, other, {Title: check.Expect}}, 3},
		{"miss", []discovery.SearchResult{other}, -1},
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
	events []discovery.InteractionEvent
}

func (r *recordingEventStore) Append(_ context.Context, e discovery.InteractionEvent) error {
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

func (stubSearchProvider) Name() discovery.ProviderName { return discovery.ProviderDeezer }

func (stubSearchProvider) Search(_ context.Context, _ string, _ map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
	return nil, nil
}

func (stubSearchProvider) SupportedKinds() map[discovery.ResultKind]bool {
	return map[discovery.ResultKind]bool{
		discovery.ResultKindTrack:  true,
		discovery.ResultKindAlbum:  true,
		discovery.ResultKindArtist: true,
	}
}

func runSmokeEvalRecording(t *testing.T, user shared.UserId) int {
	t.Helper()
	store := &recordingEventStore{}
	svc := discovery.NewSearchService(
		[]discovery.SearchProvider{stubSearchProvider{}},
		discovery.NewCircuitBreaker(),
		discovery.WithEventStore(store),
	)
	if _, err := discovery.RunSmokeEval(context.Background(), svc, user); err != nil {
		t.Fatalf("Run: %v", err)
	}
	svc.WaitForBackground()
	return store.count()
}

func TestSmokeEval_RunsUnderSyntheticIdentity(t *testing.T) {
	if !shared.SystemUserId().IsSystem() {
		t.Fatal("smoke eval must run under the synthetic system identity")
	}

	if n := runSmokeEvalRecording(t, shared.NewUserId(uuid.New())); n == 0 {
		t.Fatal("real identity should persist InteractionEvents (control)")
	}

	if n := runSmokeEvalRecording(t, shared.SystemUserId()); n != 0 {
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
	_ shared.UserId,
	query *discovery.SearchQuery,
	_ bool,
) (*discovery.SearchOutput, error) {
	if s.failing[query.Raw] {
		return nil, errors.New("transient upstream failure")
	}
	return &discovery.SearchOutput{
		Results: []discovery.SearchResult{{Title: query.Raw}},
	}, nil
}

func TestSmokeEval_OneErroringQueryDoesNotDiscardOthers(t *testing.T) {
	const failing = "Drake"
	svc := searcherFailing(failing)

	res, err := discovery.RunSmokeEval(context.Background(), svc, shared.SystemUserId())
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

	res, err := discovery.RunSmokeEval(context.Background(), svc, shared.SystemUserId())

	if err == nil {
		t.Fatal("every query erroring returned no error; the meter would report a zero score instead of an error state")
	}
	if res.Regressed {
		t.Error("a run that scored no query must not claim a ranking regression")
	}
}

func TestSmokeEval_PartialOutageIsNotReportedAsARegression(t *testing.T) {
	svc := searcherFailing("Drake", "Bad Bunny")

	res, err := discovery.RunSmokeEval(context.Background(), svc, shared.SystemUserId())
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
