package service

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
)

// --- persistHistory failure tolerance ------------------------------------

func TestPersistHistory_SavesEntryAndTrimsToRing(t *testing.T) {
	var inserted *domain.SearchHistoryEntry
	var trimmedTo int
	repo := &fakeSearchHistoryRepository{
		insertFn: func(_ context.Context, e *domain.SearchHistoryEntry) error {
			inserted = e
			return nil
		},
		trimToNFn: func(_ context.Context, _ shared.UserId, n int) error {
			trimmedTo = n
			return nil
		},
	}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithHistoryRepository(repo))

	user := newUser()
	if _, err := svc.Execute(context.Background(), user, newQuery(t, "  Humble  "), true); err != nil {
		t.Fatal(err)
	}
	if inserted == nil {
		t.Fatal("saveHistory=true must insert a history entry")
	}
	if inserted.UserId != user || inserted.Query != "  Humble  " || inserted.QueryNorm != "humble" {
		t.Errorf("entry = user %v query %q norm %q", inserted.UserId, inserted.Query, inserted.QueryNorm)
	}
	if trimmedTo != historyRingSize {
		t.Errorf("TrimToN called with %d, want the %d ring size", trimmedTo, historyRingSize)
	}
}

func TestPersistHistory_SkippedWhenNotRequested(t *testing.T) {
	repo := &fakeSearchHistoryRepository{
		insertFn: func(context.Context, *domain.SearchHistoryEntry) error {
			t.Error("saveHistory=false must not insert")
			return nil
		},
	}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithHistoryRepository(repo))
	runSearch(t, svc, "humble") // saveHistory=false
}

func TestPersistHistory_InsertFailureToleratedAndSkipsTrim(t *testing.T) {
	trimCalled := false
	repo := &fakeSearchHistoryRepository{
		insertFn: func(context.Context, *domain.SearchHistoryEntry) error {
			return errors.New("db down")
		},
		trimToNFn: func(context.Context, shared.UserId, int) error {
			trimCalled = true
			return nil
		},
	}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithHistoryRepository(repo))

	out, err := svc.Execute(context.Background(), newUser(), newQuery(t, "humble"), true)
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
	repo := &fakeSearchHistoryRepository{
		trimToNFn: func(context.Context, shared.UserId, int) error {
			return errors.New("trim broke")
		},
	}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithHistoryRepository(repo))

	if _, err := svc.Execute(context.Background(), newUser(), newQuery(t, "humble"), true); err != nil {
		t.Fatalf("a trim failure must never fail the search: %v", err)
	}
}

// --- launchBackground panic recovery -------------------------------------

// panickingEventStore panics inside the background telemetry goroutine — the
// recover in launchBackground must swallow it (the process, the search, and
// WaitForBackground all survive).
type panickingEventStore struct{ calls int32 }

func (p *panickingEventStore) Append(context.Context, domain.InteractionEvent) error {
	atomic.AddInt32(&p.calls, 1)
	panic("telemetry adapter bug")
}

func TestLaunchBackground_PanicRecovered(t *testing.T) {
	store := &panickingEventStore{}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithEventStore(store))

	out := runSearch(t, svc, "humble")
	svc.WaitForBackground() // must return (Done deferred before the recover) and not re-panic

	if len(out.Results) != 1 {
		t.Fatalf("search must succeed despite the background panic, got %d results", len(out.Results))
	}
	if atomic.LoadInt32(&store.calls) != 1 {
		t.Fatalf("append called %d times, want 1 (the panicking call)", store.calls)
	}
}

// --- emitSearchEvent payload shape ---------------------------------------

func TestEmitSearchEvent_PayloadShape(t *testing.T) {
	store := &fakeEventStore{}
	results := make([]domain.SearchResult, 0, 12)
	for i := 0; i < 12; i++ {
		results = append(results, deezerTrack("Humble Take "+string(rune('A'+i)), "Artist", float64(100-i)))
	}
	p := &fakeProvider{name: domain.ProviderDeezer, results: results}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithEventStore(store), WithExploration(1.0))

	user := newUser()
	out, err := svc.Execute(context.Background(), user, newQuery(t, "humble"), false)
	if err != nil {
		t.Fatal(err)
	}
	svc.WaitForBackground()

	events := store.snapshot()
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
	// Exploration stamps for the propensity slate.
	if e.Payload["exploration"] != true {
		t.Errorf("exploration = %v, want true (rate 1.0)", e.Payload["exploration"])
	}
	if e.Payload["exploration_rate"] != 1.0 {
		t.Errorf("exploration_rate = %v, want 1.0", e.Payload["exploration_rate"])
	}
	if _, ok := e.Payload["tail_noise_top5"]; !ok {
		t.Error("payload missing tail_noise_top5")
	}
	// Top is capped at telemetryTopN and each entry carries the shown shape.
	top, ok := e.Payload["top"].([]map[string]any)
	if !ok {
		t.Fatalf("top has wrong type %T", e.Payload["top"])
	}
	if len(top) != telemetryTopN {
		t.Fatalf("top entries = %d, want capped at %d", len(top), telemetryTopN)
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
	store := &fakeEventStore{}
	p := &fakeProvider{name: domain.ProviderDeezer, results: nil}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithEventStore(store))

	runSearch(t, svc, "zxqv")
	svc.WaitForBackground()

	events := store.snapshot()
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

// --- operator seams -------------------------------------------------------

func TestRankVariantsForEval_WithAndWithoutReshape(t *testing.T) {
	// Five same-artist tracks: reshape (EnforceDiversity) caps the artist inside
	// the top window, the no-reshape baseline does not — the differential the
	// diversity harness measures. One fan-out serves both.
	results := []domain.SearchResult{
		deezerTrack("Humble One", "Kendrick Lamar", 90),
		deezerTrack("Humble Two", "Kendrick Lamar", 80),
		deezerTrack("Humble Three", "Kendrick Lamar", 70),
		deezerTrack("Humble Four", "Kendrick Lamar", 60),
		deezerTrack("Humble Five", "Other Artist", 50),
	}
	p := &countingProvider{name: domain.ProviderDeezer, results: results}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker())

	with, without := svc.RankVariantsForEval(context.Background(), newQuery(t, "humble"))

	if p.calls != 1 {
		t.Fatalf("provider fan-outs = %d, want exactly 1 (shared by both variants)", p.calls)
	}
	if len(with) == 0 || len(without) == 0 {
		t.Fatalf("variants empty: with=%d without=%d", len(with), len(without))
	}
	if len(without) != len(results) {
		t.Errorf("no-reshape variant = %d results, want all %d", len(without), len(results))
	}
	// Membership identical — reshaping reorders/caps, it never invents results.
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
	results := []domain.SearchResult{
		deezerTrack("Humble", "Kendrick Lamar", 90),
		deezerTrack("Humble Two", "Kendrick Lamar", 80),
	}
	p := &countingProvider{name: domain.ProviderDeezer, results: results}
	cache := newFakeResultCache()
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithResultCache(cache))

	// Warm the cache through the normal path.
	runSearch(t, svc, "humble")
	if p.calls != 1 || cache.sets != 1 {
		t.Fatalf("precondition: calls=%d sets=%d, want 1/1", p.calls, cache.sets)
	}

	q, err := domain.NewSearchQuery("humble", map[domain.ResultKind]bool{domain.ResultKindTrack: true}, 1)
	if err != nil {
		t.Fatal(err)
	}
	got := svc.InspectSearch(context.Background(), q)

	if p.calls != 2 {
		t.Errorf("provider calls = %d, want 2 (InspectSearch must bypass the cache)", p.calls)
	}
	if len(got) != 1 {
		t.Errorf("results = %d, want the limit=1 truncation", len(got))
	}
}

// --- maybeExplore boundaries ----------------------------------------------

func TestMaybeExplore_FewerThanTwoResultsNeverExplores(t *testing.T) {
	svc := NewService(nil, NewCircuitBreaker(), WithExploration(1.0))
	one := []domain.SearchResult{{Title: "solo"}}
	if _, explored := svc.maybeExplore(one); explored {
		t.Error("a single result cannot be reordered — must not count as explored")
	}
	if _, explored := svc.maybeExplore(nil); explored {
		t.Error("empty list must not explore")
	}
}

func TestWithExploration_NonPositiveRateIgnored(t *testing.T) {
	svc := NewService(nil, NewCircuitBreaker(), WithExploration(0), WithExploration(-0.5))
	if svc.explorationRate != 0 {
		t.Errorf("explorationRate = %v, want 0 (non-positive rates ignored)", svc.explorationRate)
	}
}

// --- option wiring --------------------------------------------------------

func TestOptions_WireTheirDependencies(t *testing.T) {
	repo := &fakeSearchHistoryRepository{}
	frs := NewFindRelatedService(nil, nil, nil)
	validator := &fakeMB{}
	svc := NewService(nil, NewCircuitBreaker(),
		WithHistoryRepository(repo),
		WithAlbumValidator(validator),
		WithFindRelatedService(frs),
		WithTailDemotion(),
		WithCrossKindProminence(),
	)
	if svc.historyRepo == nil {
		t.Error("WithHistoryRepository not wired")
	}
	if svc.albumValidator == nil {
		t.Error("WithAlbumValidator not wired")
	}
	if svc.findRelatedSvc != frs {
		t.Error("WithFindRelatedService not wired")
	}
	if !svc.tailDemotion || !svc.crossKindProminence {
		t.Error("experiment flags not set by their options")
	}
}
