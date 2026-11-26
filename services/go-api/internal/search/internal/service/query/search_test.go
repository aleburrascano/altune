package query_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/service/artistpage"
	"altune/go-api/internal/search/internal/service/providerguard"
	querypkg "altune/go-api/internal/search/internal/service/query"
	"altune/go-api/internal/search/internal/service/vocabulary"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/clock/clocktest"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/random"
	"altune/go-api/internal/shared/random/randomtest"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	historyRingSize     = 100
	telemetryTopEntries = 10
	relatedSubjects     = 5
	breakerTripsAfter   = 5
	breakerOpenFor      = 30 * time.Second
)

func runSearch(t *testing.T, svc *search.SearchService, raw string) *search.SearchOutput {
	t.Helper()
	out, err := svc.Execute(context.Background(), memory.NewUser(), memory.NewQuery(t, raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out
}

func TestService_EndToEnd_MergesAndRanks(t *testing.T) {
	bg := searchtest.NewTracker(t)
	trackP1 := memory.WithPop(memory.WithISRC(memory.Track("HUMBLE.", "Kendrick Lamar", search.ProviderDeezer, nil), "X"), 90)
	trackP2 := memory.WithPop(memory.WithISRC(memory.Track("Humble", "Kendrick Lamar", domain.ProviderITunes, nil), "X"), 90)
	album := memory.DeezerAlbum("Humble", "Kendrick Lamar", 40)

	p1 := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{trackP1, album}}
	p2 := &memory.FakeProvider{Provider: domain.ProviderITunes, Results: []search.SearchResult{trackP2}}

	svc := searchtest.NewSearchService(t, bg, []search.SearchProvider{p1, p2}, search.NewCircuitBreaker(clock.Now))
	out := runSearch(t, svc, "humble")

	if len(out.Results) != 2 {
		t.Fatalf("want 2 results, got %d: %v", len(out.Results), memory.Titles(out.Results))
	}
	if out.Results[0].Kind != search.ResultKindTrack {
		t.Errorf("want track first, got %v", memory.Titles(out.Results))
	}
	if got := len(out.Results[0].Sources); got != 2 {
		t.Errorf("want merged track with 2 sources, got %d", got)
	}
	if out.Partial {
		t.Error("want partial=false (all providers ok)")
	}
}

func TestService_PartialOnProviderError(t *testing.T) {
	bg := searchtest.NewTracker(t)
	good := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	bad := &memory.FakeProvider{Provider: domain.ProviderITunes, Err: errors.New("boom")}

	svc := searchtest.NewSearchService(t, bg, []search.SearchProvider{good, bad}, search.NewCircuitBreaker(clock.Now))
	out := runSearch(t, svc, "humble")

	if !out.Partial {
		t.Error("want partial=true when a provider fails")
	}
	if len(out.Results) != 1 {
		t.Fatalf("want the good provider's result to survive, got %v", memory.Titles(out.Results))
	}
}

func TestService_RanksExactTitleFirst(t *testing.T) {
	bg := searchtest.NewTracker(t)
	exact := memory.DeezerTrack("HUMBLE.", "Kendrick Lamar", 40)
	partial := memory.DeezerTrack("Humble Beginnings", "Someone Else", 99)
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{partial, exact}}

	svc := searchtest.NewSearchService(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now))
	out := runSearch(t, svc, "humble")

	if len(out.Results) == 0 || out.Results[0].Title != "HUMBLE." {
		t.Fatalf("want the exact-title track first, got %v", memory.Titles(out.Results))
	}
}

func TestFanOut_CanceledParentDoesNotRecordBreakerFailure(t *testing.T) {
	bg := searchtest.NewTracker(t)
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Delay: 50 * time.Millisecond}
	cb := search.NewCircuitBreaker(clock.Now)
	svc := searchtest.NewSearchService(t, bg, []search.SearchProvider{p}, cb)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, statuses := inspect(t, svc, ctx)

	if statuses[0].Status != domain.ProviderStatusTimeout {
		t.Errorf("status = %v, want timeout (ctx canceled)", statuses[0].Status)
	}
	entry := cb.Snapshot(search.ProviderDeezer)
	if entry.Failures != 0 || entry.State != providerguard.CircuitClosed {
		t.Errorf("breaker state changed on parent cancellation: failures=%d state=%v", entry.Failures, entry.State)
	}
}

type panickingProvider struct{ name search.ProviderName }

func (p *panickingProvider) Name() search.ProviderName { return p.name }

func (p *panickingProvider) Search(_ context.Context, _ string, _ map[search.ResultKind]bool) ([]search.SearchResult, error) {
	panic("adapter bug")
}

func (p *panickingProvider) SupportedKinds() map[search.ResultKind]bool {
	return map[search.ResultKind]bool{search.ResultKindTrack: true}
}

func TestService_PanickingProviderDoesNotKillSearch(t *testing.T) {
	bg := searchtest.NewTracker(t)
	good := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	bad := &panickingProvider{name: domain.ProviderITunes}
	cb := search.NewCircuitBreaker(clock.Now)
	svc := searchtest.NewSearchService(t, bg, []search.SearchProvider{good, bad}, cb)

	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 || out.Results[0].Title != "Humble" {
		t.Fatalf("want the good provider's result to survive the panic, got %v", memory.Titles(out.Results))
	}
	if !out.Partial {
		t.Error("want partial=true (the panicking slot is a provider error)")
	}
	var panicked *domain.ProviderSearchResponse
	for i := range out.ProviderStatuses {
		if out.ProviderStatuses[i].Provider == domain.ProviderITunes {
			panicked = &out.ProviderStatuses[i]
		}
	}
	if panicked == nil || panicked.Status != domain.ProviderStatusError {
		t.Errorf("panicking slot status = %+v, want provider error", panicked)
	}
	entry := cb.Snapshot(domain.ProviderITunes)
	if entry.Failures != 1 {
		t.Errorf("panic must record a breaker failure, got %+v", entry)
	}
}

func TestService_LimitTruncates(t *testing.T) {
	bg := searchtest.NewTracker(t)
	var results []search.SearchResult
	for i := 0; i < 5; i++ {
		results = append(results, memory.DeezerTrack("Song", "Artist "+string(rune('A'+i)), float64(50-i)))
	}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: results}
	svc := searchtest.NewSearchService(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now))

	q, err := search.NewSearchQuery("song", map[search.ResultKind]bool{search.ResultKindTrack: true}, 3)
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Execute(context.Background(), ids.NewUserId(uuid.New()), q)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 3 {
		t.Fatalf("want limit=3 enforced, got %d", len(out.Results))
	}
}

func TestPersistHistory_SavesEntryAndTrimsToRing(t *testing.T) {
	bg := searchtest.NewTracker(t)
	var inserted *domain.SearchHistoryEntry
	var trimmedTo int
	repo := &memory.FakeHistoryWriter{
		InsertFn: func(_ context.Context, e *domain.SearchHistoryEntry) error {
			inserted = e
			return nil
		},
		TrimToNFn: func(_ context.Context, _ ids.UserId, n int) error {
			trimmedTo = n
			return nil
		},
	}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.History = repo })

	user := memory.NewUser()
	if _, err := svc.ExecuteRecorded(context.Background(), user, memory.NewQuery(t, "  Humble  ")); err != nil {
		t.Fatal(err)
	}
	if inserted == nil {
		t.Fatal("ExecuteRecorded must insert a history entry")
	}
	if inserted.UserId != user || inserted.Query != "  Humble  " || inserted.QueryNorm != "humble" {
		t.Errorf("entry = user %v query %q norm %q", inserted.UserId, inserted.Query, inserted.QueryNorm)
	}
	if trimmedTo != historyRingSize {
		t.Errorf("TrimToN called with %d, want the %d ring size", trimmedTo, historyRingSize)
	}
}

func TestPersistHistory_SkippedWhenNotRequested(t *testing.T) {
	bg := searchtest.NewTracker(t)
	repo := &memory.FakeHistoryWriter{
		InsertFn: func(context.Context, *domain.SearchHistoryEntry) error {
			t.Error("Execute must not insert")
			return nil
		},
	}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.History = repo })
	runSearch(t, svc, "humble")
}

func TestPersistHistory_InsertFailureToleratedAndSkipsTrim(t *testing.T) {
	bg := searchtest.NewTracker(t)
	trimCalled := false
	repo := &memory.FakeHistoryWriter{
		InsertFn: func(context.Context, *domain.SearchHistoryEntry) error {
			return errors.New("db down")
		},
		TrimToNFn: func(context.Context, ids.UserId, int) error {
			trimCalled = true
			return nil
		},
	}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.History = repo })

	out, err := svc.ExecuteRecorded(context.Background(), memory.NewUser(), memory.NewQuery(t, "humble"))
	if err != nil {
		t.Fatalf("a history-insert failure must never fail the search: %v", err)
	}
	if len(out.Results) != 1 {
		t.Fatalf("results = %d, want 1 (unaffected)", len(out.Results))
	}
	if trimCalled {
		t.Error("trim must be skipped after a failed insert (nothing new to trim)")
	}
}

func TestPersistHistory_TrimFailureTolerated(t *testing.T) {
	bg := searchtest.NewTracker(t)
	repo := &memory.FakeHistoryWriter{
		TrimToNFn: func(context.Context, ids.UserId, int) error {
			return errors.New("trim broke")
		},
	}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.History = repo })

	if _, err := svc.ExecuteRecorded(context.Background(), memory.NewUser(), memory.NewQuery(t, "humble")); err != nil {
		t.Fatalf("a trim failure must never fail the search: %v", err)
	}
}

func TestLaunchBackground_PanicRecovered(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &memory.PanickingEventStore{}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Events = store })

	out := runSearch(t, svc, "humble")
	bg.Wait()

	if len(out.Results) != 1 {
		t.Fatalf("search must succeed despite the background panic, got %d results", len(out.Results))
	}
	if atomic.LoadInt32(&store.Calls) != 1 {
		t.Fatalf("append called %d times, want 1 (the panicking call)", store.Calls)
	}
}

func TestEmitSearchEvent_PayloadShape(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &memory.FakeEventStore{}
	results := make([]search.SearchResult, 0, 12)
	for i := 0; i < 12; i++ {
		results = append(results, memory.DeezerTrack("Humble Take "+string(rune('A'+i)), "Artist", float64(100-i)))
	}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: results}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Events = store }, querypkg.WithExploration(1.0))

	user := memory.NewUser()
	out, err := svc.Execute(context.Background(), user, memory.NewQuery(t, "humble"))
	if err != nil {
		t.Fatal(err)
	}
	bg.Wait()

	events := store.Snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	e := events[0]
	if e.UserId != user || e.QueryNorm != "humble" || e.SearchId != out.SearchId {
		t.Errorf("envelope = user %v norm %q searchId %q (want output's %q)", e.UserId, e.QueryNorm, e.SearchId, out.SearchId)
	}
	if e.Payload["result_count"] != len(out.Results) {
		t.Errorf("result_count = %v, want %d", e.Payload["result_count"], len(out.Results))
	}
	if e.Payload["exploration"] != true {
		t.Errorf("exploration = %v, want true (rate 1.0)", e.Payload["exploration"])
	}
	if e.Payload["exploration_rate"] != 1.0 {
		t.Errorf("exploration_rate = %v, want 1.0", e.Payload["exploration_rate"])
	}
	if _, ok := e.Payload["tail_noise_top5"]; !ok {
		t.Error("payload missing tail_noise_top5")
	}
	top, ok := e.Payload["top"].([]map[string]any)
	if !ok {
		t.Fatalf("top has wrong type %T", e.Payload["top"])
	}
	if len(top) != telemetryTopEntries {
		t.Fatalf("top entries = %d, want capped at %d", len(top), telemetryTopEntries)
	}
	first := top[0]
	if first["position"] != 0 || first["kind"] != "track" {
		t.Errorf("top[0] = %v, want position 0 kind track", first)
	}
	if first["title"] != out.Results[0].Title || first["subtitle"] != out.Results[0].Subtitle {
		t.Errorf("top[0] title/subtitle = %v/%v, want the SHOWN (explored) order's %q/%q",
			first["title"], first["subtitle"], out.Results[0].Title, out.Results[0].Subtitle)
	}
	sources, ok := first["sources"].([]string)
	if !ok || len(sources) != 1 || sources[0] != "deezer" {
		t.Errorf("top[0] sources = %v, want [deezer]", first["sources"])
	}
}

func TestEmitSearchEvent_ZeroResultPayload(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &memory.FakeEventStore{}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: nil}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Events = store })

	runSearch(t, svc, "zxqv")
	bg.Wait()

	events := store.Snapshot()
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	e := events[0]
	if e.Payload["zero_result"] != true || e.Payload["result_count"] != 0 {
		t.Errorf("payload = %v, want zero_result=true result_count=0", e.Payload)
	}
	if _, ok := e.Payload["top"]; ok {
		t.Error("zero-result payload must omit top")
	}
	if _, ok := e.Payload["exploration"]; ok {
		t.Error("non-explored search must omit the exploration stamp")
	}
}

func TestRankVariantsForEval_WithAndWithoutReshape(t *testing.T) {
	bg := searchtest.NewTracker(t)
	results := []search.SearchResult{
		memory.DeezerTrack("Humble One", "Kendrick Lamar", 90),
		memory.DeezerTrack("Humble Two", "Kendrick Lamar", 80),
		memory.DeezerTrack("Humble Three", "Kendrick Lamar", 70),
		memory.DeezerTrack("Humble Four", "Kendrick Lamar", 60),
		memory.DeezerTrack("Humble Five", "Other Artist", 50),
	}
	p := &memory.CountingProvider{Provider: search.ProviderDeezer, Results: results}
	svc := searchtest.NewSearchService(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now))

	with, without := svc.RankVariantsForEval(context.Background(), memory.NewQuery(t, "humble"))

	if p.Calls != 1 {
		t.Fatalf("provider fan-outs = %d, want exactly 1 (shared by both variants)", p.Calls)
	}
	if len(with) == 0 || len(without) == 0 {
		t.Fatalf("variants empty: with=%d without=%d", len(with), len(without))
	}
	if len(without) != len(results) {
		t.Errorf("no-reshape variant = %d results, want all %d", len(without), len(results))
	}
	seen := map[string]bool{}
	for _, r := range without {
		seen[r.Title] = true
	}
	for _, r := range with {
		if !seen[r.Title] {
			t.Errorf("reshaped variant invented %q", r.Title)
		}
	}
}

func TestInspectSearch_BypassesResultCacheAndTruncates(t *testing.T) {
	bg := searchtest.NewTracker(t)
	results := []search.SearchResult{
		memory.DeezerTrack("Humble", "Kendrick Lamar", 90),
		memory.DeezerTrack("Humble Two", "Kendrick Lamar", 80),
	}
	p := &memory.CountingProvider{Provider: search.ProviderDeezer, Results: results}
	cache := memory.NewFakeResultCache()
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.ResultCache = cache })

	runSearch(t, svc, "humble")
	if p.Calls != 1 || cache.Sets != 1 {
		t.Fatalf("precondition: calls=%d sets=%d, want 1/1", p.Calls, cache.Sets)
	}

	q, err := search.NewSearchQuery("humble", map[search.ResultKind]bool{search.ResultKindTrack: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := svc.InspectSearchWithStatuses(context.Background(), q)

	if p.Calls != 2 {
		t.Errorf("provider calls = %d, want 2 (inspection must bypass the cache)", p.Calls)
	}
	if len(got) != 1 {
		t.Errorf("results = %d, want the limit=1 truncation", len(got))
	}
}

func TestMaybeExplore_FewerThanTwoResultsNeverExplores(t *testing.T) {
	bg := searchtest.NewTracker(t)
	svc := searchtest.NewSearchService(t, bg, nil, search.NewCircuitBreaker(clock.Now), querypkg.WithExploration(1.0))
	one := []search.SearchResult{{Title: "solo"}}
	if _, explored := svc.MaybeExplore(one); explored {
		t.Error("a single result cannot be reordered — must not count as explored")
	}
	if _, explored := svc.MaybeExplore(nil); explored {
		t.Error("empty list must not explore")
	}
}

func TestMaybeExplore_ShufflesByTheGivenDrawsAndStaysPutWhenTheDrawMissesTheRate(t *testing.T) {
	bg := searchtest.NewTracker(t)
	three := []search.SearchResult{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	titles := func(rs []search.SearchResult) string {
		var out string
		for _, r := range rs {
			out += r.Title
		}
		return out
	}
	cases := map[string]struct {
		draws        random.Unit
		wantExplored bool
		wantOrder    string
	}{
		"a draw at or above the rate leaves the order": {randomtest.Fixed(0.5), false, "abc"},
		"draws below the rate shuffle by them":         {randomtest.Sequence(0.1, 0.0, 0.0), true, "bca"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			svc := searchtest.NewSearchServiceWith(t, bg, nil, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.ExplorationDraw = tc.draws }, querypkg.WithExploration(0.5))

			got, explored := svc.MaybeExplore(three)

			if explored != tc.wantExplored || titles(got) != tc.wantOrder {
				t.Fatalf("MaybeExplore = (%s, %v), want (%s, %v)", titles(got), explored, tc.wantOrder, tc.wantExplored)
			}
		})
	}
}

func TestOptions_OutOfRangeValuesAreRefused(t *testing.T) {
	for name, opt := range map[string]querypkg.Option{
		"zero exploration rate":     querypkg.WithExploration(0),
		"negative exploration rate": querypkg.WithExploration(-0.5),
		"zero deadline":             querypkg.WithSearchDeadline(0),
		"negative deadline":         querypkg.WithSearchDeadline(-time.Second),
	} {
		_, err := querypkg.NewService(searchtest.NewTracker(t), nil, search.NewCircuitBreaker(clock.Now), querypkg.AbsentCollaborators(), opt)
		if !errors.Is(err, querypkg.ErrInvalidOption) {
			t.Errorf("%s: err = %v, want query.ErrInvalidOption", name, err)
		}
	}
}

func TestNewService_RefusesEveryMissingCollaborator(t *testing.T) {
	bg := searchtest.NewTracker(t)
	cb := search.NewCircuitBreaker(clock.Now)
	for name, build := range map[string]func() (*search.SearchService, error){
		"tracker": func() (*search.SearchService, error) {
			return querypkg.NewService(nil, nil, cb, querypkg.AbsentCollaborators())
		},
		"circuitBreaker": func() (*search.SearchService, error) {
			return querypkg.NewService(bg, nil, nil, querypkg.AbsentCollaborators())
		},
		"artwork":         withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.Artwork = nil }),
		"related":         withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.Related = nil }),
		"history":         withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.History = nil }),
		"vocabulary":      withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.Vocabulary = nil }),
		"events":          withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.Events = nil }),
		"activity":        withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.Activity = nil }),
		"favorites":       withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.Favorites = nil }),
		"resultCache":     withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.ResultCache = nil }),
		"providerCache":   withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.ProviderCache = nil }),
		"heldSlates":      withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.HeldSlates = nil }),
		"behavior":        withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.Behavior = nil }),
		"explorationDraw": withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.ExplorationDraw = nil }),
		"deadlineTimer":   withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.DeadlineTimer = nil }),
		"now":             withoutCollaborator(bg, cb, func(c *search.Collaborators) { c.Now = nil }),
	} {
		svc, err := build()
		if !errors.Is(err, querypkg.ErrCollaboratorRequired) || svc != nil {
			t.Errorf("without %s: svc, err = %v, %v, want query.ErrCollaboratorRequired", name, svc, err)
			continue
		}
		if !strings.Contains(err.Error(), name) {
			t.Errorf("without %s: err = %q, want it to name the missing collaborator", name, err)
		}
	}
}

func withoutCollaborator(bg *background.Tracker, cb *providerguard.CircuitBreaker, drop func(*search.Collaborators)) func() (*search.SearchService, error) {
	return func() (*search.SearchService, error) {
		c := querypkg.AbsentCollaborators()
		drop(&c)
		return querypkg.NewService(bg, nil, cb, c)
	}
}

func TestNewService_AllAbsentCollaboratorsServeASearchUnchanged(t *testing.T) {
	bg := searchtest.NewTracker(t)
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 90)}}
	svc, err := querypkg.NewService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), querypkg.AbsentCollaborators())
	if err != nil {
		t.Fatalf("NewSearchService: %v", err)
	}
	out := runSearch(t, svc, "humble")
	if len(out.Results) != 1 || out.Results[0].Title != "Humble" || len(out.Related) != 0 || out.Results[0].ArtworkPath != "" {
		t.Fatalf("results = %v related = %v, want the one provider result untouched", memory.Titles(out.Results), out.Related)
	}
}

func rankedArtists(t *testing.T, providers []search.SearchProvider, opts ...querypkg.Option) []string {
	t.Helper()
	bg := searchtest.NewTracker(t)
	return memory.Subtitles(runSearch(t, searchtest.NewSearchService(t, bg, providers, search.NewCircuitBreaker(clock.Now), opts...), "humble").Results)
}

func TestWithTailDemotion_RanksAnUnidentifiedSoundCloudTrackLast(t *testing.T) {
	providers := func() []search.SearchProvider {
		return []search.SearchProvider{
			&memory.FakeProvider{Provider: search.ProviderSoundCloud, Results: []search.SearchResult{
				memory.WithPop(memory.Track("Humble", "Tail Artist", search.ProviderSoundCloud, nil), 90),
			}},
			&memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{
				memory.DeezerTrack("Humble", "Solid Artist", 10),
			}},
		}
	}
	if got, want := rankedArtists(t, providers()), []string{"Tail Artist", "Solid Artist"}; !slices.Equal(got, want) {
		t.Fatalf("without the option: %v, want %v (the more popular track first)", got, want)
	}
	if got, want := rankedArtists(t, providers(), querypkg.WithTailDemotion()), []string{"Solid Artist", "Tail Artist"}; !slices.Equal(got, want) {
		t.Errorf("with tail demotion: %v, want %v", got, want)
	}
}

func TestWithCrossKindProminence_RanksTheMoreProminentTrackFirst(t *testing.T) {
	prominent := memory.DeezerTrack("Humble", "Prominent Artist", 10)
	prominent.ProviderRank = 1000
	popular := memory.DeezerTrack("Humble", "Popular Artist", 90)
	popular.ProviderRank = 1
	providers := func() []search.SearchProvider {
		return []search.SearchProvider{&memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{popular, prominent}}}
	}
	if got, want := rankedArtists(t, providers()), []string{"Popular Artist", "Prominent Artist"}; !slices.Equal(got, want) {
		t.Fatalf("without the option: %v, want %v (the more popular track first)", got, want)
	}
	if got, want := rankedArtists(t, providers(), querypkg.WithCrossKindProminence()), []string{"Prominent Artist", "Popular Artist"}; !slices.Equal(got, want) {
		t.Errorf("with cross-kind prominence: %v, want %v", got, want)
	}
}

type driftingProvider struct {
	name   search.ProviderName
	slates [][]search.SearchResult
	calls  int
}

func (p *driftingProvider) Name() search.ProviderName { return p.name }

func (p *driftingProvider) Search(_ context.Context, _ string, _ map[search.ResultKind]bool) ([]search.SearchResult, error) {
	slate := p.slates[min(p.calls, len(p.slates)-1)]
	p.calls++
	return slate, nil
}

func (p *driftingProvider) SupportedKinds() map[search.ResultKind]bool {
	return map[search.ResultKind]bool{search.ResultKindTrack: true}
}

func artistRun(title, prefix string, n int) []search.SearchResult {
	out := make([]search.SearchResult, n)
	for i := range out {
		out[i] = memory.DeezerTrack(title, fmt.Sprintf("%s %02d", prefix, i), float64(90-i))
	}
	return out
}

func pagingService(t *testing.T, slates ...[]search.SearchResult) (*search.SearchService, *driftingProvider) {
	t.Helper()
	bg := searchtest.NewTracker(t)
	drifting := &driftingProvider{name: search.ProviderDeezer, slates: slates}
	down := &memory.CountingProvider{Provider: domain.ProviderITunes, Err: errors.New("upstream down")}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{drifting, down}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		cache := memory.NewFakeResultCache()
		c.ResultCache, c.HeldSlates = cache, cache
	})
	return svc, drifting
}

func searchPage(
	t *testing.T,
	svc *search.SearchService,
	userId ids.UserId,
	raw string,
	offset, limit int,
	continues ids.EntityId,
) *search.SearchOutput {
	t.Helper()
	query, err := search.NewSearchQuery(raw, map[search.ResultKind]bool{search.ResultKindTrack: true}, limit)
	if err != nil {
		t.Fatal(err)
	}
	query.Offset = offset
	out, err := svc.ExecutePage(context.Background(), userId, query, continues)
	if err != nil {
		t.Fatalf("ExecutePage(offset=%d): %v", offset, err)
	}
	return out
}

func searchIdOf(t *testing.T, out *search.SearchOutput) ids.EntityId {
	t.Helper()
	id, err := ids.ParseEntityId(out.SearchId)
	if err != nil {
		t.Fatalf("SearchId %q is not a uuid: %v", out.SearchId, err)
	}
	return id
}

func TestService_PagesOfOneSearchComeFromOneRanking(t *testing.T) {
	svc, _ := pagingService(t, artistRun("Humble", "Alpha", 12), artistRun("Humble", "Bravo", 12))
	reference, _ := pagingService(t, artistRun("Humble", "Alpha", 12))
	userId := memory.NewUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, ids.EntityId{})
	second := searchPage(t, svc, userId, "humble", 5, 5, searchIdOf(t, first))

	paged := append(memory.Subtitles(first.Results), memory.Subtitles(second.Results)...)
	unpaged := memory.Subtitles(searchPage(t, reference, memory.NewUser(), "humble", 0, 10, ids.EntityId{}).Results)
	if len(paged) != len(unpaged) {
		t.Fatalf("paged through %d results, want the %d of one ranking: %v", len(paged), len(unpaged), paged)
	}
	for i, artist := range unpaged {
		if paged[i] != artist {
			t.Fatalf("position %d = %q, want %q (paged %v, one ranking %v)", i, paged[i], artist, paged, unpaged)
		}
	}
	if second.SearchId != first.SearchId {
		t.Errorf("second page reports search %q, want the continued %q", second.SearchId, first.SearchId)
	}
	if second.Total != first.Total {
		t.Errorf("total = %d on page two, %d on page one", second.Total, first.Total)
	}
}

func TestService_PageAskedForWithoutASearchIdRanksAfresh(t *testing.T) {
	svc, drifting := pagingService(t, artistRun("Humble", "Alpha", 12), artistRun("Humble", "Bravo", 12))
	userId := memory.NewUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, ids.EntityId{})
	second := searchPage(t, svc, userId, "humble", 5, 5, ids.EntityId{})

	if drifting.calls != 2 {
		t.Errorf("provider calls = %d, want 2: a page naming no search fans out as before", drifting.calls)
	}
	if len(second.Results) != 5 {
		t.Fatalf("page two served %d results, want 5: %v", len(second.Results), memory.Subtitles(second.Results))
	}
	if second.SearchId == first.SearchId {
		t.Error("a page cut from a fresh ranking must report a new search id")
	}
}

func TestService_SearchIdReplayedAgainstAnotherQueryServesThatQuery(t *testing.T) {
	svc, _ := pagingService(t, artistRun("Humble", "Alpha", 12), artistRun("Loyalty", "Bravo", 12))
	userId := memory.NewUser()

	humble := searchPage(t, svc, userId, "humble", 0, 5, ids.EntityId{})
	loyalty := searchPage(t, svc, userId, "loyalty", 0, 5, searchIdOf(t, humble))

	if len(loyalty.Results) == 0 {
		t.Fatal("the borrowed id served nothing, so this proves nothing about which slate it read")
	}
	for _, artist := range memory.Subtitles(loyalty.Results) {
		if !strings.HasPrefix(artist, "Bravo") {
			t.Fatalf("a search id replayed against another query served %v, want that query's own fan-out",
				memory.Subtitles(loyalty.Results))
		}
	}
	if loyalty.SearchId == humble.SearchId {
		t.Error("a query that borrowed another search's id must report its own")
	}
}

func TestService_Execute_DoesNotLogQueryText(t *testing.T) {
	bg := searchtest.NewTracker(t)
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const raw = "zqxj private diagnosis clinic"
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{
		memory.Track("Clinic", "Someone", search.ProviderDeezer, nil),
	}}
	svc := searchtest.NewSearchService(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now))
	runSearch(t, svc, raw)

	logged := buf.String()
	if !strings.Contains(logged, "search.v2.start") || !strings.Contains(logged, "search.v2.complete") {
		t.Fatalf("expected search lifecycle logs, got:\n%s", logged)
	}
	for _, form := range []string{raw, "diagnosis", "zqxj"} {
		if strings.Contains(logged, form) {
			t.Fatalf("log output contains search text %q:\n%s", form, logged)
		}
	}
}

func TestExecute_SystemIdentityDoesNotTouchFavoritesOrTelemetry(t *testing.T) {
	favorites := []domain.Favorite{memory.FavoriteOf(search.ResultKindArtist, "Kendrick Lamar", "")}
	results := []search.SearchResult{
		memory.DeezerTrack("Humble", "Kendrick Lamar", 80),
		memory.DeezerTrack("Humble Beginnings", "Kendrick Lamar", 70),
	}

	t.Run("real identity is personalized and recorded (control)", func(t *testing.T) {
		store, favs, history := runExecuteAs(t, memory.NewUser(), favorites, results)
		if len(store.Snapshot()) == 0 {
			t.Error("real identity should persist an InteractionEvent")
		}
		if favs.ListCalls == 0 {
			t.Error("real identity should consult favorites")
		}
		if len(history) == 0 {
			t.Error("real identity should persist its search history")
		}
	})

	t.Run("system identity is neither personalized nor recorded", func(t *testing.T) {
		store, favs, history := runExecuteAs(t, ids.SystemUserId(), favorites, results)
		if got := len(store.Snapshot()); got != 0 {
			t.Errorf("system identity persisted %d InteractionEvents; want 0", got)
		}
		if favs.ListCalls != 0 {
			t.Errorf("system identity consulted favorites %d times; want 0", favs.ListCalls)
		}
		if got := len(history); got != 0 {
			t.Errorf("system identity persisted %d history entries; want 0", got)
		}
	})
}

func runExecuteAs(
	t *testing.T,
	user ids.UserId,
	favorites []domain.Favorite,
	results []search.SearchResult,
) (*memory.FakeEventStore, *memory.FakeFavoritesRepo, []*domain.SearchHistoryEntry) {
	t.Helper()
	bg := searchtest.NewTracker(t)
	store := &memory.FakeEventStore{}
	favs := &memory.FakeFavoritesRepo{Favorites: favorites}
	var recorded []*domain.SearchHistoryEntry
	history := &memory.FakeHistoryWriter{
		InsertFn: func(_ context.Context, entry *domain.SearchHistoryEntry) error {
			recorded = append(recorded, entry)
			return nil
		},
	}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: results}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Events = store; c.Favorites = favs; c.History = history })
	if _, err := svc.ExecuteRecorded(context.Background(), user, memory.NewQuery(t, "humble")); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	bg.Wait()
	return store, favs, recorded
}

func TestMaybeExplore_DisabledIsInert(t *testing.T) {
	bg := searchtest.NewTracker(t)
	svc := searchtest.NewSearchService(t, bg, nil, search.NewCircuitBreaker(clock.Now))
	in := []search.SearchResult{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	out, explored := svc.MaybeExplore(in)
	if explored {
		t.Error("exploration must be off when rate is 0")
	}
	if &out[0] != &in[0] {
		t.Error("inert path must return the same slice, not a copy")
	}
}

func TestIngestVocabulary_UsesOrganicOrderNotExploredShuffle(t *testing.T) {
	bg := searchtest.NewTracker(t)
	results := make([]search.SearchResult, 0, 20)
	for i := 0; i < 20; i++ {
		results = append(results, memory.DeezerTrack("Humble Take "+string(rune('A'+i)), "Artist "+string(rune('A'+i)), float64(100-i)))
	}
	newSvc := func(store *memory.FakeVocabularyStore, opts ...querypkg.Option) *search.SearchService {
		p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: results}
		return searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Vocabulary = store }, opts...)
	}
	capture := func(store *memory.FakeVocabularyStore) *[]string {
		var mu sync.Mutex
		terms := &[]string{}
		store.AddFn = func(e domain.SpellingHintsEntry) error {
			mu.Lock()
			defer mu.Unlock()
			*terms = append(*terms, e.Term)
			return nil
		}
		return terms
	}

	for run := 0; run < 5; run++ {
		organicStore, exploredStore := &memory.FakeVocabularyStore{}, &memory.FakeVocabularyStore{}
		organicTerms := capture(organicStore)
		exploredTerms := capture(exploredStore)

		organicSvc := newSvc(organicStore)
		exploredSvc := newSvc(exploredStore, querypkg.WithExploration(1.0))

		runSearch(t, organicSvc, "humble")
		bg.Wait()
		out := runSearch(t, exploredSvc, "humble")
		bg.Wait()

		if !out.Explored {
			t.Fatal("precondition: rate 1.0 must explore")
		}
		if len(*organicTerms) == 0 {
			t.Fatal("precondition: organic run ingested nothing")
		}
		if len(*organicTerms) != len(*exploredTerms) {
			t.Fatalf("run %d: ingest lengths differ: organic %v vs explored %v", run, *organicTerms, *exploredTerms)
		}
		for i := range *organicTerms {
			if (*organicTerms)[i] != (*exploredTerms)[i] {
				t.Fatalf("run %d: explored search ingested the shuffled slate, not the organic top:\norganic  %v\nexplored %v",
					run, *organicTerms, *exploredTerms)
			}
		}
	}
}

func TestMaybeExplore_AlwaysExploresClonesAndKeepsMembers(t *testing.T) {
	bg := searchtest.NewTracker(t)
	svc := searchtest.NewSearchService(t, bg, nil, search.NewCircuitBreaker(clock.Now), querypkg.WithExploration(1.0))
	in := []search.SearchResult{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	out, explored := svc.MaybeExplore(in)

	if !explored {
		t.Fatal("rate 1.0 must always explore")
	}
	if in[0].Title != "a" || in[1].Title != "b" || in[2].Title != "c" {
		t.Error("maybeExplore must not mutate the input (cache) slice")
	}
	seen := map[string]bool{}
	for _, r := range out {
		seen[r.Title] = true
	}
	if len(out) != 3 || !seen["a"] || !seen["b"] || !seen["c"] {
		t.Errorf("exploration must preserve membership, got %v", out)
	}
}

func expiringSlateService(t *testing.T, slates ...[]search.SearchResult) (*search.SearchService, *driftingProvider, *memory.FakeResultCache) {
	t.Helper()
	bg := searchtest.NewTracker(t)
	drifting := &driftingProvider{name: search.ProviderDeezer, slates: slates}
	down := &memory.CountingProvider{Provider: domain.ProviderITunes, Err: errors.New("upstream down")}
	heldCache := memory.NewFakeResultCache()
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{drifting, down}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.ResultCache = memory.NewFakeResultCache(); c.HeldSlates = heldCache })
	return svc, drifting, heldCache
}

func TestService_PagingSurvivesAHeldSlateExpiring(t *testing.T) {
	svc, drifting, heldCache := expiringSlateService(
		t,
		artistRun("Humble", "Alpha", 12),
		artistRun("Humble", "Bravo", 12),
	)
	userId := memory.NewUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, ids.EntityId{})
	if drifting.calls != 1 {
		t.Fatalf("provider calls after page one = %d, want 1", drifting.calls)
	}

	heldCache.Store = map[string][]search.SearchResult{}

	second := searchPage(t, svc, userId, "humble", 5, 5, searchIdOf(t, first))
	if second.SearchId == first.SearchId {
		t.Fatal("a page served after the held slate expired must report a new search id")
	}
	if drifting.calls != 2 {
		t.Fatalf("provider calls after the expired page = %d, want 2 (one fresh fan-out)", drifting.calls)
	}

	third := searchPage(t, svc, userId, "humble", 10, 5, searchIdOf(t, second))
	if third.SearchId != second.SearchId {
		t.Fatalf("third page reports %q, want the ranking page two just re-established %q",
			third.SearchId, second.SearchId)
	}
	if drifting.calls != 2 {
		t.Fatalf("provider calls after page three = %d, want 2: the fresh ranking page two "+
			"produced must be held for page three too, not re-fanned-out", drifting.calls)
	}

	all := append(memory.Subtitles(second.Results), memory.Subtitles(third.Results)...)
	seen := map[string]bool{}
	for _, artist := range all {
		if seen[artist] {
			t.Fatalf("artist %q shown twice across pages two and three: %v", artist, all)
		}
		seen[artist] = true
	}
}

func TestService_SearchEmitsActivityWithoutQueryText(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &memory.FakeEventStore{}
	feed := &memory.RecordingActivityFeed{}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Alright", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Events = store; c.Activity = feed })

	runSearch(t, svc, "alright")
	bg.Wait()

	if got := feed.Recorded(); len(got) != 1 || got[0] != "search_performed" {
		t.Errorf("feed activity = %v, want [search_performed]", got)
	}
}

func TestService_ExecutePageReportsWhetherTheSlateCameFromCache(t *testing.T) {
	bg := searchtest.NewTracker(t)
	p := &memory.CountingProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.ResultCache = memory.NewFakeResultCache() })
	userId := memory.NewUser()

	fresh := searchPage(t, svc, userId, "humble", 0, 5, ids.EntityId{})
	if fresh.Cached {
		t.Error("a fresh fan-out reported Cached = true")
	}
	repeated := searchPage(t, svc, userId, "humble", 0, 5, ids.EntityId{})
	if !repeated.Cached {
		t.Error("a repeat answered from the result cache reported Cached = false")
	}
}

func TestService_ExecutePageReportsCachedForAHeldSlate(t *testing.T) {
	svc, _, _ := expiringSlateService(t, artistRun("Humble", "Alpha", 12))
	userId := memory.NewUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, ids.EntityId{})
	if first.Cached {
		t.Error("the first page reported Cached = true")
	}
	second := searchPage(t, svc, userId, "humble", 5, 5, searchIdOf(t, first))
	if !second.Cached {
		t.Error("a page cut from the held slate reported Cached = false")
	}
}

type gatedProvider struct {
	memory.FakeProvider
	calls   atomic.Int32
	release chan struct{}
}

func (p *gatedProvider) Search(ctx context.Context, q string, k map[search.ResultKind]bool) ([]search.SearchResult, error) {
	p.calls.Add(1)
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return p.FakeProvider.Search(ctx, q, k)
}

func newCollapseService(t *testing.T, release chan struct{}) (*search.SearchService, *gatedProvider) {
	bg := searchtest.NewTracker(t)
	p := &gatedProvider{
		FakeProvider: memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}},
		release:      release,
	}
	return searchtest.NewSearchService(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now)), p
}

func TestService_Search_ColdStampedeSharesOneFanOut(t *testing.T) {
	release := make(chan struct{})
	svc, p := newCollapseService(t, release)
	const callers = 10
	outs := make([]*search.SearchOutput, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := svc.Execute(context.Background(), memory.NewUser(), memory.NewQuery(t, "humble"))
			if err != nil {
				t.Errorf("Execute: %v", err)
				return
			}
			outs[i] = out
		}()
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()

	if got := p.calls.Load(); got != 1 {
		t.Errorf("provider reached %d times for %d concurrent callers, want 1", got, callers)
	}
	for i, out := range outs {
		if out == nil || out.Partial || len(out.Results) != 1 {
			t.Errorf("caller %d got %+v, want one non-partial result", i, out)
		}
	}
}

func TestService_Search_CancelledCallerDoesNotFailSharedFanOut(t *testing.T) {
	release := make(chan struct{})
	svc, _ := newCollapseService(t, release)
	cancelCtx, cancel := context.WithCancel(context.Background())
	done := make(chan *search.SearchOutput, 1)
	go func() {
		out, _ := svc.Execute(cancelCtx, memory.NewUser(), memory.NewQuery(t, "humble"))
		done <- out
	}()
	time.Sleep(50 * time.Millisecond)
	survivor := make(chan *search.SearchOutput, 1)
	go func() {
		out, _ := svc.Execute(context.Background(), memory.NewUser(), memory.NewQuery(t, "humble"))
		survivor <- out
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	<-done
	close(release)

	out := <-survivor
	if out == nil || out.Partial || len(out.Results) != 1 {
		t.Errorf("surviving caller got %+v, want one non-partial result", out)
	}
}

type panickingIdentityStore struct{}

func (panickingIdentityStore) LookupByProviderID(context.Context, search.ResultKind, domain.ProviderKey, string) (string, map[domain.ProviderKey]string, bool) {
	panic("identity store bug")
}

func (panickingIdentityStore) LookupByProviderIDs(context.Context, []ports.IdentityRef) map[ports.IdentityRef]ports.IdentityHit {
	panic("identity store bug")
}

func TestService_PanicBelowResolveColdDegradesToPartial(t *testing.T) {
	bg := searchtest.NewTracker(t)
	p := &memory.FakeProvider{
		Provider: search.ProviderDeezer,
		Results:  []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)},
		Delay:    150 * time.Millisecond,
	}
	cache := memory.NewFakeResultCache()
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		c.ResultCache = cache
		c.Artwork = searchtest.NewArtworkFill(t, bg, searchtest.UseArtworkLookup(&memory.ArtworkLookup{}), searchtest.UseIdentityStore(panickingIdentityStore{}))
	})

	const callers = 2
	outs := make([]*search.SearchOutput, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outs[i], errs[i] = svc.Execute(context.Background(), memory.NewUser(), memory.NewQuery(t, "humble"))
		}()
	}
	wg.Wait()

	for i := range callers {
		if errs[i] != nil {
			t.Fatalf("caller %d: Execute: %v", i, errs[i])
		}
		if !outs[i].Partial {
			t.Errorf("caller %d: want partial=true after a panic in resolveCold", i)
		}
	}
	if cache.Sets != 0 {
		t.Errorf("a degraded answer was cached: sets=%d", cache.Sets)
	}
}

func TestService_PagesTileOneRankingWithoutGapsOrRepeats(t *testing.T) {
	const total, limit = 23, 5
	svc, _ := pagingService(t, artistRun("Humble", "Alpha", total))
	reference, _ := pagingService(t, artistRun("Humble", "Alpha", total))
	unpaged := memory.Subtitles(searchPage(t, reference, memory.NewUser(), "humble", 0, 50, ids.EntityId{}).Results)
	if len(unpaged) != total {
		t.Fatalf("one ranking holds %d results, want %d", len(unpaged), total)
	}
	userId := memory.NewUser()
	first := searchPage(t, svc, userId, "humble", 0, limit, ids.EntityId{})
	id := searchIdOf(t, first)

	paged := memory.Subtitles(first.Results)
	for offset := limit; offset <= total+limit; offset += limit {
		page := searchPage(t, svc, userId, "humble", offset, limit, id)
		if want := max(0, min(limit, total-offset)); len(page.Results) != want {
			t.Fatalf("page at offset %d served %d results, want %d: a short last page, then nothing", offset, len(page.Results), want)
		}
		paged = append(paged, memory.Subtitles(page.Results)...)
	}

	if !slices.Equal(paged, unpaged) {
		t.Fatalf("pages = %v, want the one ranking %v", paged, unpaged)
	}
}

type stopObservingProvider struct {
	memory.FakeProvider
	started chan struct{}
	stopped chan struct{}
}

func newStopObservingProvider() *stopObservingProvider {
	return &stopObservingProvider{
		FakeProvider: memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}},
		started:      make(chan struct{}, 8),
		stopped:      make(chan struct{}, 8),
	}
}

func (p *stopObservingProvider) Search(ctx context.Context, q string, k map[search.ResultKind]bool) ([]search.SearchResult, error) {
	p.started <- struct{}{}
	<-ctx.Done()
	p.stopped <- struct{}{}
	return nil, ctx.Err()
}

func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestService_Search_LastCallerCancelStopsProviderCalls(t *testing.T) {
	bg := searchtest.NewTracker(t)
	p := newStopObservingProvider()
	svc := searchtest.NewSearchService(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now))
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = svc.Execute(ctx, memory.NewUser(), memory.NewQuery(t, "humble"))
		close(done)
	}()
	waitFor(t, p.started, "the provider call to start")

	cancel()

	waitFor(t, done, "the cancelled caller to return")
	waitFor(t, p.stopped, "the provider call to stop after its only caller left")
}

func TestService_Search_CallerAfterAbandonedSearchGetsAFreshAnswer(t *testing.T) {
	release := make(chan struct{})
	svc, p := newCollapseService(t, release)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = svc.Execute(ctx, memory.NewUser(), memory.NewQuery(t, "humble"))
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	waitFor(t, done, "the cancelled caller to return")

	close(release)
	out, err := svc.Execute(context.Background(), memory.NewUser(), memory.NewQuery(t, "humble"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Partial || len(out.Results) != 1 {
		t.Errorf("caller after an abandoned search got partial=%v results=%d, want a complete answer", out.Partial, len(out.Results))
	}
	if got := p.calls.Load(); got != 2 {
		t.Errorf("provider reached %d times, want 2 (the abandoned run, then a fresh one)", got)
	}
}

func TestService_Search_LogsHowLongEachStageTook(t *testing.T) {
	bg := searchtest.NewTracker(t)
	buf := memory.CaptureLogs(t)
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}, Delay: 30 * time.Millisecond}
	svc := searchtest.NewSearchService(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now))

	runSearch(t, svc, "humble")

	var complete map[string]any
	if err := json.Unmarshal([]byte(memory.LoggedEvent(t, buf, "search.v2.complete")), &complete); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"fanout_ms", "rank_ms", "artwork_ms", "favorites_ms", "related_ms", "total_ms"} {
		if _, ok := complete[stage].(float64); !ok {
			t.Errorf("search.v2.complete has no %s: %v", stage, complete)
		}
	}
	if ms, _ := complete["fanout_ms"].(float64); ms < 30 {
		t.Errorf("fanout_ms = %v, want at least the provider's 30ms", ms)
	}
	if total, _ := complete["total_ms"].(float64); total < complete["fanout_ms"].(float64) {
		t.Errorf("total_ms = %v, want at least fanout_ms", total)
	}
}

func TestService_SkipsEnrichWhenArtworkPresent(t *testing.T) {
	bg := searchtest.NewTracker(t)
	resolver := &memory.ArtworkLookup{URL: "https://art/new.jpg"}
	withArt := memory.DeezerTrack("Humble", "Kendrick Lamar", 80)
	withArt.ImageURL = "https://existing/art.jpg"
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{withArt}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		c.Artwork = searchtest.NewArtworkFill(t, bg, searchtest.UseArtworkLookup(resolver))
	})

	out := runSearch(t, svc, "humble")

	if out.Results[0].ImageURL != "https://existing/art.jpg" {
		t.Errorf("artwork = %q, want the existing image kept", out.Results[0].ImageURL)
	}
	if n := resolver.Calls(); n != 0 {
		t.Errorf("resolver called %d times, want 0 (track already had art)", n)
	}
}

func TestService_MBIDIndexAttachesMBIDForArtwork(t *testing.T) {
	bg := searchtest.NewTracker(t)
	resolver := &capturingArtworkLookup{url: "https://caa/hd.jpg"}
	idx := &fakeMBIDIndex{mbid: "warm-mbid"}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		c.Artwork = searchtest.NewArtworkFill(t, bg, searchtest.UseArtworkLookup(resolver), searchtest.UseArtworkCache(&memory.FakeArtworkCache{Store: map[string]string{}}), searchtest.UseMBIDIndex(idx))
	})

	runSearch(t, svc, "humble")
	bg.Wait()
	out := runSearch(t, svc, "humble")

	if out.Results[0].ImageURL != "https://caa/hd.jpg" {
		t.Errorf("artwork = %q, want resolved HD", out.Results[0].ImageURL)
	}
	if resolver.gotMBID != "warm-mbid" {
		t.Errorf("resolver got mbid %q, want the warmed MBID attached", resolver.gotMBID)
	}
	if atomic.LoadInt32(&idx.lookups) == 0 {
		t.Error("MBID index was never consulted")
	}
}

func TestService_IdentityStoreResolvesArtworkWhenMBAbsent(t *testing.T) {
	bg := searchtest.NewTracker(t)
	resolver := &capturingArtworkLookup{url: "https://caa/right-face.jpg"}
	store := &memory.FakeIdentityStore{Mbid: "durable-mbid", Xref: map[domain.ProviderKey]string{"discogs": "123"}}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		c.Artwork = searchtest.NewArtworkFill(t, bg, searchtest.UseArtworkLookup(resolver), searchtest.UseArtworkCache(&memory.FakeArtworkCache{Store: map[string]string{}}), searchtest.UseIdentityStore(store))
	})

	out := runSearch(t, svc, "humble")
	bg.Wait()

	if atomic.LoadInt32(&store.Lookups) == 0 {
		t.Error("identity store was never consulted")
	}
	if resolver.gotMBID != "durable-mbid" {
		t.Errorf("resolver got mbid %q, want the durable MBID attached from the store", resolver.gotMBID)
	}
	if out.Results[0].Xref["discogs"] != "123" {
		t.Errorf("xref = %v, want the bridged ids attached from the store", out.Results[0].Xref)
	}
}

func TestService_BackgroundArtworkUsesTheDurableIdentity(t *testing.T) {
	bg := searchtest.NewTracker(t)
	resolver := &fakeIdentityAwareResolver{url: "https://caa/right.jpg"}
	cache := &memory.FakeArtworkCache{Store: map[string]string{}}
	store := &memory.FakeIdentityStore{Mbid: "durable-mbid", Xref: map[domain.ProviderKey]string{"discogs": "123"}}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		c.Artwork = searchtest.NewArtworkFill(t, bg, searchtest.UseArtworkLookup(resolver), searchtest.UseArtworkCache(cache), searchtest.UseIdentityStore(store))
	})

	runSearch(t, svc, "humble")
	bg.Wait()

	if got := cache.Store["Humble"]; got != "https://caa/right.jpg" {
		t.Errorf("cached artwork = %q, want the identity-resolved image", got)
	}
}

func TestService_ArtworkCacheShortCircuits(t *testing.T) {
	bg := searchtest.NewTracker(t)
	resolver := &memory.ArtworkLookup{URL: "https://art/resolved.jpg"}
	cache := &memory.FakeArtworkCache{Store: map[string]string{"Humble": "https://art/cached.jpg"}}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		c.Artwork = searchtest.NewArtworkFill(t, bg, searchtest.UseArtworkLookup(resolver), searchtest.UseArtworkCache(cache))
	})

	out := runSearch(t, svc, "humble")

	if out.Results[0].ImageURL != "https://art/cached.jpg" {
		t.Errorf("artwork = %q, want the cached image", out.Results[0].ImageURL)
	}
	if n := resolver.Calls(); n != 0 {
		t.Errorf("resolver called %d times, want 0 (cache hit)", n)
	}
}

func TestFillArtwork_PanickingResolverIsContained(t *testing.T) {
	bg := searchtest.NewTracker(t)
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		c.Artwork = searchtest.NewArtworkFill(t, bg, searchtest.UseArtworkLookup(&panickingArtworkLookup{}))
	})

	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 || out.Results[0].Title != "Humble" {
		t.Fatalf("results = %+v, want the original Humble result kept", out.Results)
	}
}

func TestService_MissingArtworkIsFetchedAfterTheAnswer(t *testing.T) {
	bg := searchtest.NewTracker(t)
	resolver := &gatedArtworkLookup{url: "https://caa/cover.jpg", release: make(chan struct{})}
	cache := &memory.FakeArtworkCache{Store: map[string]string{}}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		c.Artwork = searchtest.NewArtworkFill(t, bg, searchtest.UseArtworkLookup(resolver), searchtest.UseArtworkCache(cache))
	})

	first := runSearch(t, svc, "humble")

	if first.Results[0].ImageURL != "" {
		t.Fatalf("first answer ImageURL = %q, want none while the live lookup runs", first.Results[0].ImageURL)
	}
	if path := first.Results[0].ArtworkPath; path != domain.ArtworkPathPending {
		t.Errorf("ArtworkPath = %q, want pending", path)
	}

	close(resolver.release)
	bg.Wait()

	second := runSearch(t, svc, "humble")
	if second.Results[0].ImageURL != "https://caa/cover.jpg" {
		t.Errorf("next answer ImageURL = %q, want the cover the background found", second.Results[0].ImageURL)
	}
}

func TestService_WithoutAnArtworkCacheNoLiveLookupRuns(t *testing.T) {
	bg := searchtest.NewTracker(t)
	resolver := &memory.ArtworkLookup{URL: "https://art/cover.jpg"}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		c.Artwork = searchtest.NewArtworkFill(t, bg, searchtest.UseArtworkLookup(resolver))
	})

	runSearch(t, svc, "humble")
	bg.Wait()

	if n := resolver.Calls(); n != 0 {
		t.Errorf("resolver called %d times, want 0 (nowhere to keep the answer)", n)
	}
}

type capturingArtworkLookup struct {
	url     string
	mu      sync.Mutex
	gotMBID string
}

func (r *capturingArtworkLookup) ResolveTagged(_ context.Context, _ search.ResultKind, _, _, mbid string) (string, domain.ProviderKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if mbid != "" {
		r.gotMBID = mbid
	}
	return r.url, "", nil
}

func (r *capturingArtworkLookup) ResolveWithIdentityTagged(_ context.Context, _ search.ResultKind, _, _ string, _ ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	return "", "", nil
}

type fakeMBIDIndex struct {
	mbid    string
	lookups int32
}

func (f *fakeMBIDIndex) LookupMBID(_ context.Context, _ search.ResultKind, _ string) (string, bool) {
	atomic.AddInt32(&f.lookups, 1)
	if f.mbid == "" {
		return "", false
	}
	return f.mbid, true
}

func (f *fakeMBIDIndex) RememberMBID(_ context.Context, _ search.ResultKind, _, _ string) error {
	return nil
}

type fakeIdentityAwareResolver struct{ url string }

func (r *fakeIdentityAwareResolver) ResolveTagged(_ context.Context, _ search.ResultKind, _, _, _ string) (string, domain.ProviderKey, error) {
	return "", "", nil
}

func (r *fakeIdentityAwareResolver) ResolveWithIdentityTagged(_ context.Context, _ search.ResultKind, _, _ string, id ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	if id.HasLinks() {
		return r.url, "", nil
	}
	return "", "", nil
}

type panickingArtworkLookup struct{ memory.ArtworkLookup }

func (panickingArtworkLookup) ResolveTagged(context.Context, search.ResultKind, string, string, string) (string, domain.ProviderKey, error) {
	panic("artwork resolver exploded")
}

type gatedArtworkLookup struct {
	url     string
	release chan struct{}
}

func (r *gatedArtworkLookup) ResolveTagged(ctx context.Context, _ search.ResultKind, _, _, _ string) (string, domain.ProviderKey, error) {
	select {
	case <-r.release:
		return r.url, "caa", nil
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

func (r *gatedArtworkLookup) ResolveWithIdentityTagged(_ context.Context, _ search.ResultKind, _, _ string, _ ports.ArtworkIdentity) (string, domain.ProviderKey, error) {
	return "", "", nil
}

func tripViaSearch(t *testing.T, cb *providerguard.CircuitBreaker, provider search.ProviderName) {
	t.Helper()
	bg := searchtest.NewTracker(t)
	svc := searchtest.NewSearchService(t, bg, []search.SearchProvider{&memory.FakeProvider{Provider: provider, Err: memory.ErrUpstreamDown}}, cb)
	for range breakerTripsAfter {
		inspect(t, svc, context.Background())
	}
	if cb.GetStatus(provider) != domain.ProviderStatusCircuitOpen {
		t.Fatalf("setup: %s circuit not open after %d search failures", provider, breakerTripsAfter)
	}
}

func breakerTrack(provider search.ProviderName, id string) search.SearchResult {
	return memory.TrackFrom(provider, id, "Humble", "Kendrick Lamar")
}

func TestContentBreaker_SearchTrippedProviderShortCircuitsContent(t *testing.T) {
	cases := []struct {
		name     string
		provider search.ProviderName
		run      func(cb *providerguard.CircuitBreaker, calls *atomic.Int32) (*providerguard.ContentFetchResponse, error)
	}{
		{
			name:     "top tracks",
			provider: search.ProviderDeezer,
			run: func(cb *providerguard.CircuitBreaker, calls *atomic.Int32) (*providerguard.ContentFetchResponse, error) {
				p := &memory.ArtistContentProvider{GetTopTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
					calls.Add(1)
					return []search.SearchResult{breakerTrack(search.ProviderDeezer, "1")}, nil
				}}
				svc := searchtest.NewArtistContent(t, map[search.ProviderName]ports.ArtistContentProvider{search.ProviderDeezer: p}, searchtest.ArtistBreaker(cb))
				return svc.GetTopTracks(context.Background(), search.ProviderDeezer, "42", nil, 10)
			},
		},
		{
			name:     "albums",
			provider: search.ProviderDeezer,
			run: func(cb *providerguard.CircuitBreaker, calls *atomic.Int32) (*providerguard.ContentFetchResponse, error) {
				p := &memory.ArtistContentProvider{GetAlbumsFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
					calls.Add(1)
					return []search.SearchResult{memory.V2Album(search.ProviderDeezer, "a", "DAMN.")}, nil
				}}
				svc := searchtest.NewArtistContent(t, map[search.ProviderName]ports.ArtistContentProvider{search.ProviderDeezer: p}, searchtest.ArtistBreaker(cb))
				return svc.GetAlbums(context.Background(), search.ProviderDeezer, "42", nil, 10)
			},
		},
		{
			name:     "identity fan-out (v2) albums",
			provider: search.ProviderDeezer,
			run: func(cb *providerguard.CircuitBreaker, calls *atomic.Int32) (*providerguard.ContentFetchResponse, error) {
				p := &memory.ArtistContentProvider{GetAlbumsFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
					calls.Add(1)
					return []search.SearchResult{memory.V2Album(search.ProviderDeezer, "a", "DAMN.")}, nil
				}}
				svc := searchtest.NewArtistContent(t, map[search.ProviderName]ports.ArtistContentProvider{search.ProviderDeezer: p}, searchtest.ArtistIdentities(&memory.FakeIdentityStore{}), searchtest.ArtistBreaker(cb))
				return svc.GetAlbums(context.Background(), search.ProviderDeezer, "42", nil, 10)
			},
		},
		{
			name:     "album tracks with deezer search fallback",
			provider: search.ProviderDeezer,
			run: func(cb *providerguard.CircuitBreaker, calls *atomic.Int32) (*providerguard.ContentFetchResponse, error) {
				p := &memory.AlbumContentProvider{GetAlbumTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
					calls.Add(1)
					return []search.SearchResult{breakerTrack(search.ProviderDeezer, "1")}, nil
				}}
				searcher := &memory.CountingProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.AlbumSearchResult("Kendrick Lamar", "9")}}
				svc := searchtest.NewAlbumTracks(t, map[search.ProviderName]ports.AlbumContentProvider{search.ProviderDeezer: p}, searchtest.AlbumFallbackSearcher(searcher), searchtest.AlbumBreaker(cb))
				resp, err := svc.ExecuteRequest(context.Background(), artistpage.AlbumTracksRequest{
					Provider: search.ProviderDeezer, ExternalID: "7", Title: "DAMN.", Artist: "Kendrick Lamar",
				})
				calls.Add(int32(searcher.Calls))
				return resp, err
			},
		},
		{
			name:     "related tracks",
			provider: search.ProviderSoundCloud,
			run: func(cb *providerguard.CircuitBreaker, calls *atomic.Int32) (*providerguard.ContentFetchResponse, error) {
				p := &countingRelatedProvider{calls: calls}
				svc := searchtest.NewRelatedTracksWithBreaker(t, map[string]ports.RelatedTracksProvider{"soundcloud": p}, cb)
				return svc.Execute(context.Background(), search.ProviderSoundCloud, "7", 10)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := search.NewCircuitBreaker(clock.Now)
			tripViaSearch(t, cb, tc.provider)

			var calls atomic.Int32
			resp, err := tc.run(cb, &calls)
			if err != nil {
				t.Fatalf("error = %v", err)
			}
			if n := calls.Load(); n != 0 {
				t.Errorf("open-circuit provider %s was called %d times by the content endpoint, want 0", tc.provider, n)
			}
			if len(resp.Items) != 0 {
				t.Errorf("items = %d, want none from a short-circuited provider", len(resp.Items))
			}
		})
	}
}

func TestContentBreaker_SingleProviderOpenCircuitReportsCircuitOpen(t *testing.T) {
	cb := search.NewCircuitBreaker(clock.Now)
	tripViaSearch(t, cb, search.ProviderDeezer)
	svc := searchtest.NewArtistContent(t, map[search.ProviderName]ports.ArtistContentProvider{
		search.ProviderDeezer: &memory.ArtistContentProvider{},
	}, searchtest.ArtistBreaker(cb))

	resp, err := svc.GetTopTracks(context.Background(), search.ProviderDeezer, "42", nil, 10)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.Status != domain.ProviderStatusCircuitOpen {
		t.Errorf("status = %v, want circuit_open", resp.Status)
	}
}

func TestContentBreaker_ContentFailuresTripSearch(t *testing.T) {
	bg := searchtest.NewTracker(t)
	cb := search.NewCircuitBreaker(clock.Now)
	var contentCalls atomic.Int32
	p := &memory.ArtistContentProvider{GetTopTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
		contentCalls.Add(1)
		return nil, memory.ErrUpstreamDown
	}}
	content := searchtest.NewArtistContent(t, map[search.ProviderName]ports.ArtistContentProvider{search.ProviderDeezer: p}, searchtest.ArtistBreaker(cb))
	for range breakerTripsAfter + 2 {
		if _, err := content.GetTopTracks(context.Background(), search.ProviderDeezer, "42", nil, 10); err != nil {
			t.Fatalf("error = %v", err)
		}
	}
	if n := contentCalls.Load(); n != breakerTripsAfter {
		t.Errorf("content calls = %d, want %d (calls after the circuit opened are skipped)", n, breakerTripsAfter)
	}

	searcher := &memory.CountingProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{breakerTrack(search.ProviderDeezer, "1")}}
	_, statuses := inspect(t, searchtest.NewSearchService(t, bg, []search.SearchProvider{searcher}, cb), context.Background())
	if searcher.Calls != 0 || statuses[0].Status != domain.ProviderStatusCircuitOpen {
		t.Errorf("search calls = %d status = %v, want 0 calls and circuit_open after content failures",
			searcher.Calls, statuses[0].Status)
	}
}

func TestContentBreaker_OnlyHealthFailuresCount(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantOpen bool
	}{
		{"transport error", memory.ErrUpstreamDown, true},
		{"provider timeout", fmt.Errorf("fetch: %w", context.DeadlineExceeded), true},
		{"5xx", fmt.Errorf("musicbrainz status 503: %w", memory.UpstreamStatusError(503)), true},
		{"429", memory.UpstreamStatusError(429), true},
		{"404 for an unknown id", memory.UpstreamStatusError(404), false},
		{"provider data error for a bogus id", errors.New("deezer api error 800 (DataException): no data"), false},
		{"canceled", context.Canceled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := search.NewCircuitBreaker(clock.Now)
			p := &memory.FakeRelatedProvider{Err: tc.err}
			svc := searchtest.NewRelatedTracksWithBreaker(t, map[string]ports.RelatedTracksProvider{"soundcloud": p}, cb)
			for range breakerTripsAfter * 2 {
				_, _ = svc.Execute(context.Background(), search.ProviderSoundCloud, "bogus", 10)
			}
			if open := cb.GetStatus(search.ProviderSoundCloud) == domain.ProviderStatusCircuitOpen; open != tc.wantOpen {
				t.Errorf("circuit open = %v, want %v", open, tc.wantOpen)
			}
		})
	}
}

func TestContentBreaker_HalfOpenProbeOutcomes(t *testing.T) {
	cases := []struct {
		name      string
		cancel    bool
		err       error
		panics    bool
		wantState providerguard.CircuitState
	}{
		{name: "success closes", wantState: providerguard.CircuitClosed},
		{name: "failure reopens", err: memory.ErrUpstreamDown, wantState: providerguard.CircuitOpen},
		{name: "caller cancel releases", cancel: true, err: memory.ErrUpstreamDown, wantState: providerguard.CircuitHalfOpen},
		{name: "request-scoped error releases", err: memory.UpstreamStatusError(404), wantState: providerguard.CircuitHalfOpen},
		{name: "panic reopens", panics: true, wantState: providerguard.CircuitOpen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := search.NewCircuitBreaker(clock.Now)
			tripToHalfOpenWindow(t, cb, search.ProviderDeezer)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &memory.ArtistContentProvider{GetAlbumsFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
				if tc.cancel {
					cancel()
				}
				if tc.panics {
					panic("boom")
				}
				return []search.SearchResult{memory.V2Album(search.ProviderDeezer, "a", "DAMN.")}, tc.err
			}}
			svc := searchtest.NewArtistContent(t, map[search.ProviderName]ports.ArtistContentProvider{search.ProviderDeezer: p}, searchtest.ArtistIdentities(&memory.FakeIdentityStore{}), searchtest.ArtistBreaker(cb))

			_, _ = svc.GetAlbums(ctx, search.ProviderDeezer, "42", nil, 10)

			entry := cb.Snapshot(search.ProviderDeezer)
			if entry.State != tc.wantState {
				t.Errorf("state = %v, want %v", entry.State, tc.wantState)
			}
			if entry.State == providerguard.CircuitHalfOpen {
				if _, ok := cb.AllowRequest(context.Background(), search.ProviderDeezer); !ok {
					t.Error("probe slot still held: next request not admitted")
				}
			}
		})
	}
}

type countingRelatedProvider struct {
	calls *atomic.Int32
}

func (p *countingRelatedProvider) GetRelatedTracks(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
	p.calls.Add(1)
	return []search.SearchResult{breakerTrack(search.ProviderSoundCloud, "1")}, nil
}

type persistentAuthError struct{ memory.UpstreamStatusError }

func (persistentAuthError) PersistentAuthFailure() bool { return true }

type badPayloadError struct{ error }

func (badPayloadError) BadPayload() bool { return true }

func TestContentBreaker_PersistentAuthAndBadPayloadTripBreaker(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantOpen bool
	}{
		{"persistent auth failure", fmt.Errorf("top tracks: %w", persistentAuthError{401}), true},
		{"bad payload on a 200", badPayloadError{errors.New("invalid character '<'")}, true},
		{"bare 401", memory.UpstreamStatusError(401), false},
		{"bare 403", memory.UpstreamStatusError(403), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := search.NewCircuitBreaker(clock.Now)
			p := &memory.ArtistContentProvider{GetTopTracksFn: func(context.Context, search.ProviderName, string) ([]search.SearchResult, error) {
				return nil, tc.err
			}}
			content := searchtest.NewArtistContent(t, map[search.ProviderName]ports.ArtistContentProvider{search.ProviderDeezer: p}, searchtest.ArtistBreaker(cb))
			for range breakerTripsAfter {
				_, _ = content.GetTopTracks(context.Background(), search.ProviderDeezer, "42", nil, 10)
			}
			if open := cb.GetStatus(search.ProviderDeezer) == domain.ProviderStatusCircuitOpen; open != tc.wantOpen {
				t.Errorf("circuit open = %v, want %v", open, tc.wantOpen)
			}
		})
	}
}

func libraryTitles(out *search.SearchOutput) []string {
	var titles []string
	for _, group := range out.Related {
		if group.Relationship != domain.RelationshipLibraryMatches {
			continue
		}
		for _, item := range group.Items {
			titles = append(titles, item.Title)
		}
	}
	return titles
}

func searchAs(t *testing.T, svc *search.SearchService, userId ids.UserId, raw string) *search.SearchOutput {
	t.Helper()
	out, err := svc.Execute(context.Background(), userId, memory.NewQuery(t, raw))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out
}

func TestService_RepeatedSearch_ServesRelatedGroupsWithoutRefetching(t *testing.T) {
	bg := searchtest.NewTracker(t)
	organic, tracksByAlbum := relatedSearchFixture()
	querier := &memory.CountingRelationshipQuerier{}
	albumProvider := &memory.CountingAlbumTracksProvider{TracksByAlbum: tracksByAlbum}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{&memory.FakeProvider{Provider: search.ProviderDeezer, Results: organic}}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		c.ResultCache = memory.NewFakeResultCache()
		c.Related = searchtest.NewFindRelated(t, searchtest.RelatedLibrary(querier), searchtest.RelatedAlbumTracks(albumProvider))
	})
	user := memory.NewUser()

	first := searchAs(t, svc, user, "humble")
	providerCalls, libraryCalls := albumProvider.Calls.Load(), querier.Calls.Load()
	if providerCalls != relatedSubjects || libraryCalls != relatedSubjects {
		t.Fatalf("first search: provider calls = %d, library queries = %d, want %d each",
			providerCalls, libraryCalls, relatedSubjects)
	}

	second := searchAs(t, svc, user, "humble")

	if got := albumProvider.Calls.Load() - providerCalls; got != 0 {
		t.Errorf("second search: provider calls = %d, want 0", got)
	}
	if got := querier.Calls.Load() - libraryCalls; got != 0 {
		t.Errorf("second search: library queries = %d, want 0", got)
	}
	if len(second.Related) != len(first.Related) {
		t.Errorf("second search: related groups = %d, want %d (same slate, same groups)",
			len(second.Related), len(first.Related))
	}
}

func TestService_RepeatedSearch_KeepsRelatedLibraryMatchesPerUser(t *testing.T) {
	bg := searchtest.NewTracker(t)
	organic, tracksByAlbum := relatedSearchFixture()
	owner, other := memory.NewUser(), memory.NewUser()
	querier := &memory.CountingRelationshipQuerier{
		MatchesByUser: map[ids.UserId][]ports.RelatedTrackMatch{
			owner: {{Title: "Owner Library Track", Artist: "Artist 0", Album: "Album 0"}},
			other: {{Title: "Other Library Track", Artist: "Artist 0", Album: "Album 0"}},
		},
	}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{&memory.FakeProvider{Provider: search.ProviderDeezer, Results: organic}}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		c.ResultCache = memory.NewFakeResultCache()
		c.Related = searchtest.NewFindRelated(t, searchtest.RelatedLibrary(querier), searchtest.RelatedAlbumTracks(&memory.CountingAlbumTracksProvider{TracksByAlbum: tracksByAlbum}))
	})

	searchAs(t, svc, owner, "humble")
	otherOut := searchAs(t, svc, other, "humble")

	got := libraryTitles(otherOut)
	if len(got) == 0 {
		t.Fatal("second user got no library matches")
	}
	for _, title := range got {
		if title != "Other Library Track" {
			t.Errorf("second user was served %q, want only its own library matches", title)
		}
	}
}

type ownedTrackRow struct {
	owner ids.UserId
	match ports.RelatedTrackMatch
}

type ownershipQuerier struct {
	rows []ownedTrackRow
}

func (q *ownershipQuerier) FindRelatedByAlbumArtist(_ context.Context, userId ids.UserId, album string, _ string, _ int) ([]ports.RelatedTrackMatch, error) {
	var out []ports.RelatedTrackMatch
	for _, r := range q.rows {
		if r.owner == userId && r.match.Album == album {
			out = append(out, r.match)
		}
	}
	return out, nil
}

func TestSearch_RelatedLibraryMatches_NeverLeakAnotherUsersLibrary(t *testing.T) {
	bg := searchtest.NewTracker(t)
	userA := ids.NewUserId(uuid.New())
	userB := ids.NewUserId(uuid.New())
	const album = "Shared Album"

	querier := &ownershipQuerier{rows: []ownedTrackRow{
		{owner: userA, match: ports.RelatedTrackMatch{Title: "A Private Song", Artist: "Band", Album: album}},
		{owner: userB, match: ports.RelatedTrackMatch{Title: "B Own Song", Artist: "Band", Album: album}},
	}}

	organic := memory.DeezerTrack("Hit Single", "Band", 80)
	organic.Album = album
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{organic}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) {
		c.Related = searchtest.NewFindRelated(t, searchtest.RelatedLibrary(querier))
	})

	out, err := svc.Execute(context.Background(), userB, memory.NewQuery(t, "hit single"))
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	var sawOwn bool
	for _, g := range out.Related {
		for _, item := range g.Items {
			if item.Extras["source"] != "library" {
				continue
			}
			if item.Title == "A Private Song" {
				t.Errorf("user B's search leaked user A's library track %q in group %q", item.Title, g.Relationship)
			}
			if item.Title == "B Own Song" {
				sawOwn = true
			}
		}
	}
	if !sawOwn {
		t.Error("user B's own library track should still surface as a library match")
	}
}

func relatedSearchFixture() ([]search.SearchResult, map[string][]search.SearchResult) {
	var organic []search.SearchResult
	tracksByAlbum := map[string][]search.SearchResult{}
	for i := range relatedSubjects {
		albumID := fmt.Sprintf("al-%d", i)
		track := memory.TrackResult(search.ProviderDeezer, fmt.Sprintf("d-%d", i),
			fmt.Sprintf("Humble %d", i), fmt.Sprintf("Artist %d", i), nil)
		track.Album = fmt.Sprintf("Album %d", i)
		track.DeezerAlbumID = albumID
		organic = append(organic, track)
		tracksByAlbum[albumID] = []search.SearchResult{
			memory.TrackResult(search.ProviderDeezer, fmt.Sprintf("s-%d", i),
				fmt.Sprintf("Sibling %d", i), fmt.Sprintf("Artist %d", i), nil),
		}
	}
	return organic, tracksByAlbum
}

func TestSearch_RawQueryIsNeverSuggestedToAnotherUser(t *testing.T) {
	bg := searchtest.NewTracker(t)
	const private = "jane doe 42 elm street"
	vocab := &sharedVocabulary{}
	store := vocab.store()
	p := &memory.QueryFakeProvider{
		Provider: search.ProviderDeezer,
		ResultsByQuery: map[string][]search.SearchResult{
			private: {memory.DeezerTrack("Elm Street", "Jane Band", 60)},
		},
	}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Vocabulary = store })

	runSearch(t, svc, private)
	bg.Wait()

	suggest := vocabulary.NewSuggestService(store)
	for _, norm := range suggestedNorms(t, suggest, "jane") {
		if strings.Contains(norm, "doe") {
			t.Fatalf("another user's raw query leaked into suggestions: %q", norm)
		}
	}
	if got := suggestedNorms(t, suggest, "elm"); len(got) == 0 {
		t.Fatalf("want the provider-verified title still suggested, got none")
	}
}

type sharedVocabulary struct {
	mu      sync.Mutex
	entries []domain.SpellingHintsEntry
}

func (v *sharedVocabulary) store() *memory.FakeVocabularyStore {
	return &memory.FakeVocabularyStore{
		AddFn: func(e domain.SpellingHintsEntry) error {
			v.mu.Lock()
			defer v.mu.Unlock()
			v.entries = append(v.entries, e)
			return nil
		},
		SuggestByPrefixFn: func(prefix string, limit int) ([]domain.SpellingHintsEntry, error) {
			v.mu.Lock()
			defer v.mu.Unlock()
			var matches []domain.SpellingHintsEntry
			for _, e := range v.entries {
				if strings.HasPrefix(e.TermNorm, prefix) && len(matches) < limit {
					matches = append(matches, e)
				}
			}
			return matches, nil
		},
	}
}

func suggestedNorms(t *testing.T, suggest *vocabulary.SuggestService, partial string) []string {
	t.Helper()
	entries, err := suggest.Execute(context.Background(), partial, 10)
	if err != nil {
		t.Fatalf("suggest: %v", err)
	}
	norms := make([]string, 0, len(entries))
	for _, e := range entries {
		norms = append(norms, e.TermNorm)
	}
	return norms
}

type clockAdvancingProvider struct {
	manual *clocktest.Manual
	took   time.Duration
}

func (p *clockAdvancingProvider) Name() domain.ProviderName { return domain.ProviderDeezer }

func (p *clockAdvancingProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindTrack: true}
}

func (p *clockAdvancingProvider) Search(context.Context, string, map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	p.manual.Advance(p.took)
	return []domain.SearchResult{memory.Track("Humble", "Kendrick Lamar", domain.ProviderDeezer, nil)}, nil
}

func TestService_MeasuresProviderLatencyOnTheInjectedClock(t *testing.T) {
	manual := clocktest.NewManual(time.Date(2026, time.March, 4, 10, 0, 0, 0, time.UTC))
	provider := &clockAdvancingProvider{manual: manual, took: 40 * time.Millisecond}
	svc := searchtest.NewSearchServiceWith(t, searchtest.NewTracker(t), []ports.SearchProvider{provider}, search.NewCircuitBreaker(manual.Func()),
		func(c *querypkg.Collaborators) { c.Now = manual.Func() })

	out := runSearch(t, svc, "humble")

	if len(out.ProviderStatuses) != 1 {
		t.Fatalf("want 1 provider status, got %d", len(out.ProviderStatuses))
	}
	if got := out.ProviderStatuses[0].LatencyMs; got != 40 {
		t.Errorf("LatencyMs = %d, want 40 from the injected clock", got)
	}
}
