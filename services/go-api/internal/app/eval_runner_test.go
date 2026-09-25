package app

import (
	domain "altune/go-api/internal/discovery/domain"
	discoveryPorts "altune/go-api/internal/discovery/ports"
	discoveryService "altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/google/uuid"
)

func TestMatchPosition(t *testing.T) {
	results := []domain.SearchResult{
		{Title: "Bohemian Rhapsody", Subtitle: "Queen"},
		{Title: "Some Other Track", Subtitle: "Artist"},
		{Title: "Third", Subtitle: "Third Artist"},
		{Title: "HUMBLE.", Subtitle: "Kendrick Lamar"},
	}

	tests := []struct {
		name   string
		expect string
		want   int
	}{
		{"hit at position 0 (case-insensitive)", "bohemian rhapsody", 0},
		{"hit via subtitle", "queen", 0},
		{"later match reports its position", "humble", 3},
		{"miss", "nonexistent track", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchPosition(results, tt.expect); got != tt.want {
				t.Errorf("matchPosition(%q) = %d, want %d", tt.expect, got, tt.want)
			}
		})
	}
}

type recordingEventStore struct {
	mu     sync.Mutex
	events []domain.InteractionEvent
}

func (r *recordingEventStore) Append(_ context.Context, e domain.InteractionEvent) error {
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

func (stubSearchProvider) Name() domain.ProviderName { return domain.ProviderDeezer }

func (stubSearchProvider) Search(_ context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return nil, nil
}

func (stubSearchProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func runSmokeEvalRecording(t *testing.T, user shared.UserId) int {
	t.Helper()
	store := &recordingEventStore{}
	svc := discoveryService.NewService(
		[]discoveryPorts.SearchProvider{stubSearchProvider{}},
		discoveryService.NewCircuitBreaker(),
		discoveryService.WithEventStore(store),
	)
	if _, err := runSmokeEval(context.Background(), svc, user); err != nil {
		t.Fatalf("runSmokeEval: %v", err)
	}
	svc.WaitForBackground()
	return store.count()
}

// The smoke eval runs real per-user search paths, so running it as a real
// account persists InteractionEvents under that id. It must instead run under
// the synthetic system identity, which the search service refuses to record.
func TestSmokeEval_RunsUnderSyntheticIdentity(t *testing.T) {
	if !evalUserId().IsSystem() {
		t.Fatal("smoke eval must run under the synthetic system identity")
	}

	// Control: a real operator account would get an event per query (contamination).
	if n := runSmokeEvalRecording(t, shared.NewUserId(uuid.New())); n == 0 {
		t.Fatal("real identity should persist InteractionEvents (control)")
	}

	// Fix: the identity the runner actually uses persists nothing.
	if n := runSmokeEvalRecording(t, evalUserId()); n != 0 {
		t.Fatalf("smoke eval persisted %d InteractionEvents under its identity; want 0", n)
	}
}

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
