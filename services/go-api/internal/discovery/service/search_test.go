package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

type fakeProvider struct {
	name    domain.ProviderName
	results []domain.SearchResult
	err     error
	delay   time.Duration
}

func (p *fakeProvider) Name() domain.ProviderName { return p.name }

func (p *fakeProvider) Search(ctx context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	if p.delay > 0 {
		select {
		case <-time.After(p.delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return p.results, p.err
}

func (p *fakeProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}
}

func newQuery(t *testing.T, raw string) *domain.SearchQuery {
	t.Helper()
	q, err := domain.NewSearchQuery(raw, map[domain.ResultKind]bool{
		domain.ResultKindTrack:  true,
		domain.ResultKindAlbum:  true,
		domain.ResultKindArtist: true,
	}, 20)
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func newUser() shared.UserId {
	return shared.NewUserId(uuid.New())
}

func runSearch(t *testing.T, svc *Service, raw string) *SearchOutput {
	t.Helper()
	out, err := svc.Execute(context.Background(), newUser(), newQuery(t, raw), false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out
}

func TestService_EndToEnd_MergesAndRanks(t *testing.T) {
	trackP1 := withPop(withISRC(track("HUMBLE.", "Kendrick Lamar", domain.ProviderDeezer, nil), "X"), 90)
	trackP2 := withPop(withISRC(track("Humble", "Kendrick Lamar", domain.ProviderITunes, nil), "X"), 90)
	album := deezerAlbum("Humble", "Kendrick Lamar", 40)

	p1 := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{trackP1, album}}
	p2 := &fakeProvider{name: domain.ProviderITunes, results: []domain.SearchResult{trackP2}}

	svc := NewService([]ports.SearchProvider{p1, p2}, NewCircuitBreaker())
	out := runSearch(t, svc, "humble")

	if len(out.Results) != 2 {
		t.Fatalf("want 2 results, got %d: %v", len(out.Results), titles(out.Results))
	}
	if out.Results[0].Kind != domain.ResultKindTrack {
		t.Errorf("want track first, got %v", titles(out.Results))
	}
	if got := len(out.Results[0].Sources); got != 2 {
		t.Errorf("want merged track with 2 sources, got %d", got)
	}
	if out.Partial {
		t.Error("want partial=false (all providers ok)")
	}
}

func TestService_PartialOnProviderError(t *testing.T) {
	good := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	bad := &fakeProvider{name: domain.ProviderITunes, err: errors.New("boom")}

	svc := NewService([]ports.SearchProvider{good, bad}, NewCircuitBreaker())
	out := runSearch(t, svc, "humble")

	if !out.Partial {
		t.Error("want partial=true when a provider fails")
	}
	if len(out.Results) != 1 {
		t.Fatalf("want the good provider's result to survive, got %v", titles(out.Results))
	}
}

func TestService_RanksExactTitleFirst(t *testing.T) {
	exact := deezerTrack("HUMBLE.", "Kendrick Lamar", 40)
	partial := deezerTrack("Humble Beginnings", "Someone Else", 99)
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{partial, exact}}

	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker())
	out := runSearch(t, svc, "humble")

	if len(out.Results) == 0 || out.Results[0].Title != "HUMBLE." {
		t.Fatalf("want the exact-title track first, got %v", titles(out.Results))
	}
}

func TestFanOut_CanceledParentDoesNotRecordBreakerFailure(t *testing.T) {
	p := &fakeProvider{name: domain.ProviderDeezer, delay: 50 * time.Millisecond}
	cb := NewCircuitBreaker()
	svc := NewService([]ports.SearchProvider{p}, cb)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, statuses := svc.fanOut(ctx, "humble", nil)

	if statuses[0].Status != domain.ProviderStatusTimeout {
		t.Errorf("status = %v, want timeout (ctx canceled)", statuses[0].Status)
	}
	cb.mu.Lock()
	entry := cb.circuits[domain.ProviderDeezer]
	cb.mu.Unlock()
	if entry != nil && (entry.failures != 0 || entry.state != CircuitClosed) {
		t.Errorf("breaker state changed on parent cancellation: failures=%d state=%v", entry.failures, entry.state)
	}
}

type panickingProvider struct{ name domain.ProviderName }

func (p *panickingProvider) Name() domain.ProviderName { return p.name }

func (p *panickingProvider) Search(_ context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	panic("adapter bug")
}

func (p *panickingProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindTrack: true}
}

func TestService_PanickingProviderDoesNotKillSearch(t *testing.T) {
	good := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	bad := &panickingProvider{name: domain.ProviderITunes}
	cb := NewCircuitBreaker()
	svc := NewService([]ports.SearchProvider{good, bad}, cb)

	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 || out.Results[0].Title != "Humble" {
		t.Fatalf("want the good provider's result to survive the panic, got %v", titles(out.Results))
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
	cb.mu.Lock()
	entry := cb.circuits[domain.ProviderITunes]
	cb.mu.Unlock()
	if entry == nil || entry.failures != 1 {
		t.Errorf("panic must record a breaker failure, got %+v", entry)
	}
}

func TestService_LimitTruncates(t *testing.T) {
	var results []domain.SearchResult
	for i := 0; i < 5; i++ {
		results = append(results, deezerTrack("Song", "Artist "+string(rune('A'+i)), float64(50-i)))
	}
	p := &fakeProvider{name: domain.ProviderDeezer, results: results}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker())

	q, err := domain.NewSearchQuery("song", map[domain.ResultKind]bool{domain.ResultKindTrack: true}, 3)
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), q, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 3 {
		t.Fatalf("want limit=3 enforced, got %d", len(out.Results))
	}
}

func TestPersistHistory_SavesEntryAndTrimsToRing(t *testing.T) {
	var inserted *domain.SearchHistoryEntry
	var trimmedTo int
	repo := &fakeHistoryWriter{
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
	repo := &fakeHistoryWriter{
		insertFn: func(context.Context, *domain.SearchHistoryEntry) error {
			t.Error("saveHistory=false must not insert")
			return nil
		},
	}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithHistoryRepository(repo))
	runSearch(t, svc, "humble")
}

func TestPersistHistory_InsertFailureToleratedAndSkipsTrim(t *testing.T) {
	trimCalled := false
	repo := &fakeHistoryWriter{
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
	repo := &fakeHistoryWriter{
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
	svc.WaitForBackground()

	if len(out.Results) != 1 {
		t.Fatalf("search must succeed despite the background panic, got %d results", len(out.Results))
	}
	if atomic.LoadInt32(&store.calls) != 1 {
		t.Fatalf("append called %d times, want 1 (the panicking call)", store.calls)
	}
}

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

func TestRankVariantsForEval_WithAndWithoutReshape(t *testing.T) {
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
	if svc.ranking.explorationRate != 0 {
		t.Errorf("explorationRate = %v, want 0 (non-positive rates ignored)", svc.ranking.explorationRate)
	}
}

func TestOptions_WireTheirDependencies(t *testing.T) {
	repo := &fakeHistoryWriter{}
	frs := NewFindRelatedService(nil, nil, nil)
	validator := &fakeMB{}
	svc := NewService(nil, NewCircuitBreaker(),
		WithHistoryRepository(repo),
		WithAlbumValidator(validator),
		WithFindRelatedService(frs),
		WithTailDemotion(),
		WithCrossKindProminence(),
	)
	if svc.history.historyRepo == nil {
		t.Error("WithHistoryRepository not wired")
	}
	if svc.disambiguator.validator == nil {
		t.Error("WithAlbumValidator not wired")
	}
	if svc.findRelatedSvc != frs {
		t.Error("WithFindRelatedService not wired")
	}
	if !svc.ranking.tailDemotion || !svc.ranking.crossKindProminence {
		t.Error("experiment flags not set by their options")
	}
}

// driftingProvider answers each fan-out with a different slate, the way a real
// provider does when its upstream re-ranks, a timeout truncates the answer, or
// a breaker drops a source between two pages of the same search.
type driftingProvider struct {
	name   domain.ProviderName
	slates [][]domain.SearchResult
	calls  int
}

func (p *driftingProvider) Name() domain.ProviderName { return p.name }

func (p *driftingProvider) Search(_ context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	slate := p.slates[min(p.calls, len(p.slates)-1)]
	p.calls++
	return slate, nil
}

func (p *driftingProvider) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindTrack: true}
}

// artistRun is one provider answer: n tracks of the same title, told apart by
// artist, so a result that came from another fan-out is visible by its prefix.
func artistRun(title, prefix string, n int) []domain.SearchResult {
	out := make([]domain.SearchResult, n)
	for i := range out {
		out[i] = deezerTrack(title, fmt.Sprintf("%s %02d", prefix, i), float64(90-i))
	}
	return out
}

// pagingService searches one provider that drifts between fan-outs and one that
// is down. The failure keeps every slate partial, which is the slate the
// query-keyed cache never stores, so a later page has nothing to continue from
// but what the first page held.
func pagingService(t *testing.T, slates ...[]domain.SearchResult) (*Service, *driftingProvider) {
	t.Helper()
	drifting := &driftingProvider{name: domain.ProviderDeezer, slates: slates}
	down := &countingProvider{name: domain.ProviderITunes, err: errors.New("upstream down")}
	svc := NewService(
		[]ports.SearchProvider{drifting, down},
		NewCircuitBreaker(),
		WithResultCache(newFakeResultCache()),
	)
	return svc, drifting
}

func searchPage(
	t *testing.T,
	svc *Service,
	userId shared.UserId,
	raw string,
	offset, limit int,
	continues uuid.UUID,
) *SearchOutput {
	t.Helper()
	query, err := domain.NewPagedSearchQuery(raw, map[domain.ResultKind]bool{domain.ResultKindTrack: true}, limit, offset)
	if err != nil {
		t.Fatal(err)
	}
	out, err := svc.ExecutePage(context.Background(), userId, query, false, continues)
	if err != nil {
		t.Fatalf("ExecutePage(offset=%d): %v", offset, err)
	}
	return out
}

func searchIdOf(t *testing.T, out *SearchOutput) uuid.UUID {
	t.Helper()
	id, err := uuid.Parse(out.SearchId)
	if err != nil {
		t.Fatalf("SearchId %q is not a uuid: %v", out.SearchId, err)
	}
	return id
}

func TestService_PagesOfOneSearchComeFromOneRanking(t *testing.T) {
	svc, _ := pagingService(t, artistRun("Humble", "Alpha", 12), artistRun("Humble", "Bravo", 12))
	reference, _ := pagingService(t, artistRun("Humble", "Alpha", 12))
	userId := newUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, uuid.Nil)
	second := searchPage(t, svc, userId, "humble", 5, 5, searchIdOf(t, first))

	paged := append(subtitles(first.Results), subtitles(second.Results)...)
	unpaged := subtitles(searchPage(t, reference, newUser(), "humble", 0, 10, uuid.Nil).Results)
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
	userId := newUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, uuid.Nil)
	second := searchPage(t, svc, userId, "humble", 5, 5, uuid.Nil)

	if drifting.calls != 2 {
		t.Errorf("provider calls = %d, want 2: a page naming no search fans out as before", drifting.calls)
	}
	if len(second.Results) != 5 {
		t.Fatalf("page two served %d results, want 5: %v", len(second.Results), subtitles(second.Results))
	}
	if second.SearchId == first.SearchId {
		t.Error("a page cut from a fresh ranking must report a new search id")
	}
}

func TestService_SearchIdReplayedAgainstAnotherQueryServesThatQuery(t *testing.T) {
	svc, _ := pagingService(t, artistRun("Humble", "Alpha", 12), artistRun("Loyalty", "Bravo", 12))
	userId := newUser()

	humble := searchPage(t, svc, userId, "humble", 0, 5, uuid.Nil)
	loyalty := searchPage(t, svc, userId, "loyalty", 0, 5, searchIdOf(t, humble))

	if len(loyalty.Results) == 0 {
		t.Fatal("the borrowed id served nothing, so this proves nothing about which slate it read")
	}
	for _, artist := range subtitles(loyalty.Results) {
		if !strings.HasPrefix(artist, "Bravo") {
			t.Fatalf("a search id replayed against another query served %v, want that query's own fan-out",
				subtitles(loyalty.Results))
		}
	}
	if loyalty.SearchId == humble.SearchId {
		t.Error("a query that borrowed another search's id must report its own")
	}
}

// TestService_Execute_DoesNotLogQueryText guards issue #1097: search text is
// erasable via clear-history, so it must never be copied into stdout logs.
func TestService_Execute_DoesNotLogQueryText(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const raw = "zqxj private diagnosis clinic"
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{
		track("Clinic", "Someone", domain.ProviderDeezer, nil),
	}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker())
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

// The smoke-eval job runs the real per-user search path with a canned query.
// When it ran as the real operator account it read that operator's favorites
// and persisted an InteractionEvent and a history row under their real id,
// contaminating their ranking and behavioral signal. The synthetic system
// identity must skip all three, while a real identity keeps its
// personalization, telemetry, and history.
func TestExecute_SystemIdentityDoesNotTouchFavoritesOrTelemetry(t *testing.T) {
	favorites := []domain.Favorite{favoriteOf(domain.ResultKindArtist, "Kendrick Lamar", "")}
	results := []domain.SearchResult{
		deezerTrack("Humble", "Kendrick Lamar", 80),
		deezerTrack("Humble Beginnings", "Kendrick Lamar", 70),
	}

	t.Run("real identity is personalized and recorded (control)", func(t *testing.T) {
		store, favs, history := runExecuteAs(t, newUser(), favorites, results, true)
		if len(store.snapshot()) == 0 {
			t.Error("real identity should persist an InteractionEvent")
		}
		if favs.listCalls == 0 {
			t.Error("real identity should consult favorites")
		}
		if len(history) == 0 {
			t.Error("real identity should persist its search history")
		}
	})

	t.Run("system identity is neither personalized nor recorded", func(t *testing.T) {
		store, favs, history := runExecuteAs(t, shared.SystemUserId(), favorites, results, true)
		if got := len(store.snapshot()); got != 0 {
			t.Errorf("system identity persisted %d InteractionEvents; want 0", got)
		}
		if favs.listCalls != 0 {
			t.Errorf("system identity consulted favorites %d times; want 0", favs.listCalls)
		}
		if got := len(history); got != 0 {
			t.Errorf("system identity persisted %d history entries; want 0", got)
		}
	})
}

func runExecuteAs(
	t *testing.T,
	user shared.UserId,
	favorites []domain.Favorite,
	results []domain.SearchResult,
	saveHistory bool,
) (*fakeEventStore, *fakeFavoritesRepo, []*domain.SearchHistoryEntry) {
	t.Helper()
	store := &fakeEventStore{}
	favs := &fakeFavoritesRepo{favorites: favorites}
	var recorded []*domain.SearchHistoryEntry
	history := &fakeHistoryWriter{
		insertFn: func(_ context.Context, entry *domain.SearchHistoryEntry) error {
			recorded = append(recorded, entry)
			return nil
		},
	}
	p := &fakeProvider{name: domain.ProviderDeezer, results: results}
	svc := NewService(
		[]ports.SearchProvider{p},
		NewCircuitBreaker(),
		WithEventStore(store),
		WithFavorites(favs),
		WithHistoryRepository(history),
	)
	if _, err := svc.Execute(context.Background(), user, newQuery(t, "humble"), saveHistory); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	svc.WaitForBackground()
	return store, favs, recorded
}

func TestMaybeExplore_DisabledIsInert(t *testing.T) {
	svc := NewService(nil, NewCircuitBreaker())
	in := []domain.SearchResult{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	out, explored := svc.maybeExplore(in)
	if explored {
		t.Error("exploration must be off when rate is 0")
	}
	if &out[0] != &in[0] {
		t.Error("inert path must return the same slice, not a copy")
	}
}

func TestIngestVocabulary_UsesOrganicOrderNotExploredShuffle(t *testing.T) {
	results := make([]domain.SearchResult, 0, 20)
	for i := 0; i < 20; i++ {
		results = append(results, deezerTrack("Humble Take "+string(rune('A'+i)), "Artist "+string(rune('A'+i)), float64(100-i)))
	}
	newSvc := func(store *fakeVocabularyStore, opts ...Option) *Service {
		p := &fakeProvider{name: domain.ProviderDeezer, results: results}
		opts = append([]Option{WithVocabularyStore(store)}, opts...)
		return NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), opts...)
	}
	capture := func(store *fakeVocabularyStore) *[]string {
		var mu sync.Mutex
		terms := &[]string{}
		store.addFn = func(e domain.VocabularyEntry) error {
			mu.Lock()
			defer mu.Unlock()
			*terms = append(*terms, e.Term)
			return nil
		}
		return terms
	}

	for run := 0; run < 5; run++ {
		organicStore, exploredStore := &fakeVocabularyStore{}, &fakeVocabularyStore{}
		organicTerms := capture(organicStore)
		exploredTerms := capture(exploredStore)

		organicSvc := newSvc(organicStore)
		exploredSvc := newSvc(exploredStore, WithExploration(1.0))

		runSearch(t, organicSvc, "humble")
		organicSvc.WaitForBackground()
		out := runSearch(t, exploredSvc, "humble")
		exploredSvc.WaitForBackground()

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
	svc := NewService(nil, NewCircuitBreaker(), WithExploration(1.0))
	in := []domain.SearchResult{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	out, explored := svc.maybeExplore(in)

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

func expiringSlateService(t *testing.T, slates ...[]domain.SearchResult) (*Service, *driftingProvider, *fakeResultCache) {
	t.Helper()
	drifting := &driftingProvider{name: domain.ProviderDeezer, slates: slates}
	down := &countingProvider{name: domain.ProviderITunes, err: errors.New("upstream down")}
	heldCache := newFakeResultCache()
	svc := NewService(
		[]ports.SearchProvider{drifting, down},
		NewCircuitBreaker(),
		WithResultCache(newFakeResultCache()),
		WithHeldSlateCache(heldCache),
	)
	return svc, drifting, heldCache
}

func TestService_PagingSurvivesAHeldSlateExpiring(t *testing.T) {
	svc, drifting, heldCache := expiringSlateService(
		t,
		artistRun("Humble", "Alpha", 12),
		artistRun("Humble", "Bravo", 12),
	)
	userId := newUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, uuid.Nil)
	if drifting.calls != 1 {
		t.Fatalf("provider calls after page one = %d, want 1", drifting.calls)
	}

	heldCache.store = map[string][]domain.SearchResult{}

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

	all := append(subtitles(second.Results), subtitles(third.Results)...)
	seen := map[string]bool{}
	for _, artist := range all {
		if seen[artist] {
			t.Fatalf("artist %q shown twice across pages two and three: %v", artist, all)
		}
		seen[artist] = true
	}
}

func TestService_SearchEmitsActivityWithoutQueryText(t *testing.T) {
	store := &fakeEventStore{}
	admin := &recordingActivityFeed{}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Alright", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithEventStore(store), WithSearchActivityFeed(admin))

	runSearch(t, svc, "alright")
	svc.WaitForBackground()

	if got := admin.recorded(); len(got) != 1 || got[0] != "search_performed" {
		t.Errorf("admin activity = %v, want [search_performed]", got)
	}
}
