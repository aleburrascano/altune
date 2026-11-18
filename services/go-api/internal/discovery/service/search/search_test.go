package search_test

import (
	"altune/go-api/internal/discovery"
	shared "altune/go-api/internal/shared/kernel"
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

func runSearch(t *testing.T, svc *discovery.SearchService, raw string) *discovery.SearchOutput {
	t.Helper()
	out, err := svc.Execute(context.Background(), discovery.NewUser(), discovery.NewQuery(t, raw), false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out
}

func TestService_EndToEnd_MergesAndRanks(t *testing.T) {
	trackP1 := discovery.WithPop(discovery.WithISRC(discovery.Track("HUMBLE.", "Kendrick Lamar", discovery.ProviderDeezer, nil), "X"), 90)
	trackP2 := discovery.WithPop(discovery.WithISRC(discovery.Track("Humble", "Kendrick Lamar", discovery.ProviderITunes, nil), "X"), 90)
	album := discovery.DeezerAlbum("Humble", "Kendrick Lamar", 40)

	p1 := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{trackP1, album}}
	p2 := &discovery.FakeProvider{Provider: discovery.ProviderITunes, Results: []discovery.SearchResult{trackP2}}

	svc := discovery.NewSearchService([]discovery.SearchProvider{p1, p2}, discovery.NewCircuitBreaker())
	out := runSearch(t, svc, "humble")

	if len(out.Results) != 2 {
		t.Fatalf("want 2 results, got %d: %v", len(out.Results), discovery.Titles(out.Results))
	}
	if out.Results[0].Kind != discovery.ResultKindTrack {
		t.Errorf("want track first, got %v", discovery.Titles(out.Results))
	}
	if got := len(out.Results[0].Sources); got != 2 {
		t.Errorf("want merged track with 2 sources, got %d", got)
	}
	if out.Partial {
		t.Error("want partial=false (all providers ok)")
	}
}

func TestService_PartialOnProviderError(t *testing.T) {
	good := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	bad := &discovery.FakeProvider{Provider: discovery.ProviderITunes, Err: errors.New("boom")}

	svc := discovery.NewSearchService([]discovery.SearchProvider{good, bad}, discovery.NewCircuitBreaker())
	out := runSearch(t, svc, "humble")

	if !out.Partial {
		t.Error("want partial=true when a provider fails")
	}
	if len(out.Results) != 1 {
		t.Fatalf("want the good provider's result to survive, got %v", discovery.Titles(out.Results))
	}
}

func TestService_RanksExactTitleFirst(t *testing.T) {
	exact := discovery.DeezerTrack("HUMBLE.", "Kendrick Lamar", 40)
	partial := discovery.DeezerTrack("Humble Beginnings", "Someone Else", 99)
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{partial, exact}}

	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker())
	out := runSearch(t, svc, "humble")

	if len(out.Results) == 0 || out.Results[0].Title != "HUMBLE." {
		t.Fatalf("want the exact-title track first, got %v", discovery.Titles(out.Results))
	}
}

func TestFanOut_CanceledParentDoesNotRecordBreakerFailure(t *testing.T) {
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Delay: 50 * time.Millisecond}
	cb := discovery.NewCircuitBreaker()
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, cb)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, statuses := inspect(t, svc, ctx)

	if statuses[0].Status != discovery.ProviderStatusTimeout {
		t.Errorf("status = %v, want timeout (ctx canceled)", statuses[0].Status)
	}
	entry := cb.Snapshot(discovery.ProviderDeezer)
	if entry.Failures != 0 || entry.State != discovery.CircuitClosed {
		t.Errorf("breaker state changed on parent cancellation: failures=%d state=%v", entry.Failures, entry.State)
	}
}

type panickingProvider struct{ name discovery.ProviderName }

func (p *panickingProvider) Name() discovery.ProviderName { return p.name }

func (p *panickingProvider) Search(_ context.Context, _ string, _ map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
	panic("adapter bug")
}

func (p *panickingProvider) SupportedKinds() map[discovery.ResultKind]bool {
	return map[discovery.ResultKind]bool{discovery.ResultKindTrack: true}
}

func TestService_PanickingProviderDoesNotKillSearch(t *testing.T) {
	good := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	bad := &panickingProvider{name: discovery.ProviderITunes}
	cb := discovery.NewCircuitBreaker()
	svc := discovery.NewSearchService([]discovery.SearchProvider{good, bad}, cb)

	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 || out.Results[0].Title != "Humble" {
		t.Fatalf("want the good provider's result to survive the panic, got %v", discovery.Titles(out.Results))
	}
	if !out.Partial {
		t.Error("want partial=true (the panicking slot is a provider error)")
	}
	var panicked *discovery.ProviderSearchResponse
	for i := range out.ProviderStatuses {
		if out.ProviderStatuses[i].Provider == discovery.ProviderITunes {
			panicked = &out.ProviderStatuses[i]
		}
	}
	if panicked == nil || panicked.Status != discovery.ProviderStatusError {
		t.Errorf("panicking slot status = %+v, want provider error", panicked)
	}
	entry := cb.Snapshot(discovery.ProviderITunes)
	if entry.Failures != 1 {
		t.Errorf("panic must record a breaker failure, got %+v", entry)
	}
}

func TestService_LimitTruncates(t *testing.T) {
	var results []discovery.SearchResult
	for i := 0; i < 5; i++ {
		results = append(results, discovery.DeezerTrack("Song", "Artist "+string(rune('A'+i)), float64(50-i)))
	}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: results}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker())

	q, err := discovery.NewSearchQuery("song", map[discovery.ResultKind]bool{discovery.ResultKindTrack: true}, 3)
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
	var inserted *discovery.SearchHistoryEntry
	var trimmedTo int
	repo := &discovery.FakeHistoryWriter{
		InsertFn: func(_ context.Context, e *discovery.SearchHistoryEntry) error {
			inserted = e
			return nil
		},
		TrimToNFn: func(_ context.Context, _ shared.UserId, n int) error {
			trimmedTo = n
			return nil
		},
	}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithHistoryRepository(repo))

	user := discovery.NewUser()
	if _, err := svc.Execute(context.Background(), user, discovery.NewQuery(t, "  Humble  "), true); err != nil {
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
	repo := &discovery.FakeHistoryWriter{
		InsertFn: func(context.Context, *discovery.SearchHistoryEntry) error {
			t.Error("saveHistory=false must not insert")
			return nil
		},
	}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithHistoryRepository(repo))
	runSearch(t, svc, "humble")
}

func TestPersistHistory_InsertFailureToleratedAndSkipsTrim(t *testing.T) {
	trimCalled := false
	repo := &discovery.FakeHistoryWriter{
		InsertFn: func(context.Context, *discovery.SearchHistoryEntry) error {
			return errors.New("db down")
		},
		TrimToNFn: func(context.Context, shared.UserId, int) error {
			trimCalled = true
			return nil
		},
	}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithHistoryRepository(repo))

	out, err := svc.Execute(context.Background(), discovery.NewUser(), discovery.NewQuery(t, "humble"), true)
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
	repo := &discovery.FakeHistoryWriter{
		TrimToNFn: func(context.Context, shared.UserId, int) error {
			return errors.New("trim broke")
		},
	}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithHistoryRepository(repo))

	if _, err := svc.Execute(context.Background(), discovery.NewUser(), discovery.NewQuery(t, "humble"), true); err != nil {
		t.Fatalf("a trim failure must never fail the search: %v", err)
	}
}

func TestLaunchBackground_PanicRecovered(t *testing.T) {
	store := &discovery.PanickingEventStore{}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithEventStore(store))

	out := runSearch(t, svc, "humble")
	svc.WaitForBackground()

	if len(out.Results) != 1 {
		t.Fatalf("search must succeed despite the background panic, got %d results", len(out.Results))
	}
	if atomic.LoadInt32(&store.Calls) != 1 {
		t.Fatalf("append called %d times, want 1 (the panicking call)", store.Calls)
	}
}

func TestEmitSearchEvent_PayloadShape(t *testing.T) {
	store := &discovery.FakeEventStore{}
	results := make([]discovery.SearchResult, 0, 12)
	for i := 0; i < 12; i++ {
		results = append(results, discovery.DeezerTrack("Humble Take "+string(rune('A'+i)), "Artist", float64(100-i)))
	}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: results}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithEventStore(store), discovery.WithExploration(1.0))

	user := discovery.NewUser()
	out, err := svc.Execute(context.Background(), user, discovery.NewQuery(t, "humble"), false)
	if err != nil {
		t.Fatal(err)
	}
	svc.WaitForBackground()

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
	store := &discovery.FakeEventStore{}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: nil}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithEventStore(store))

	runSearch(t, svc, "zxqv")
	svc.WaitForBackground()

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
	results := []discovery.SearchResult{
		discovery.DeezerTrack("Humble One", "Kendrick Lamar", 90),
		discovery.DeezerTrack("Humble Two", "Kendrick Lamar", 80),
		discovery.DeezerTrack("Humble Three", "Kendrick Lamar", 70),
		discovery.DeezerTrack("Humble Four", "Kendrick Lamar", 60),
		discovery.DeezerTrack("Humble Five", "Other Artist", 50),
	}
	p := &discovery.CountingProvider{Provider: discovery.ProviderDeezer, Results: results}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker())

	with, without := svc.RankVariantsForEval(context.Background(), discovery.NewQuery(t, "humble"))

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
	results := []discovery.SearchResult{
		discovery.DeezerTrack("Humble", "Kendrick Lamar", 90),
		discovery.DeezerTrack("Humble Two", "Kendrick Lamar", 80),
	}
	p := &discovery.CountingProvider{Provider: discovery.ProviderDeezer, Results: results}
	cache := discovery.NewFakeResultCache()
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithResultCache(cache))

	runSearch(t, svc, "humble")
	if p.Calls != 1 || cache.Sets != 1 {
		t.Fatalf("precondition: calls=%d sets=%d, want 1/1", p.Calls, cache.Sets)
	}

	q, err := discovery.NewSearchQuery("humble", map[discovery.ResultKind]bool{discovery.ResultKindTrack: true}, 1)
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
	svc := discovery.NewSearchService(nil, discovery.NewCircuitBreaker(), discovery.WithExploration(1.0))
	one := []discovery.SearchResult{{Title: "solo"}}
	if _, explored := svc.MaybeExplore(one); explored {
		t.Error("a single result cannot be reordered — must not count as explored")
	}
	if _, explored := svc.MaybeExplore(nil); explored {
		t.Error("empty list must not explore")
	}
}

func TestWithExploration_NonPositiveRateKeepsTheEarlierRate(t *testing.T) {
	svc := discovery.NewSearchService(nil, discovery.NewCircuitBreaker(), discovery.WithExploration(1.0), discovery.WithExploration(0), discovery.WithExploration(-0.5))
	two := []discovery.SearchResult{{Title: "a"}, {Title: "b"}}
	if _, explored := svc.MaybeExplore(two); !explored {
		t.Error("rate 1.0 then non-positive rates: want every search explored, the non-positive rates ignored")
	}
}

func rankedArtists(t *testing.T, providers []discovery.SearchProvider, opts ...discovery.SearchOption) []string {
	t.Helper()
	return discovery.Subtitles(runSearch(t, discovery.NewSearchService(providers, discovery.NewCircuitBreaker(), opts...), "humble").Results)
}

func TestWithTailDemotion_RanksAnUnidentifiedSoundCloudTrackLast(t *testing.T) {
	providers := func() []discovery.SearchProvider {
		return []discovery.SearchProvider{
			&discovery.FakeProvider{Provider: discovery.ProviderSoundCloud, Results: []discovery.SearchResult{
				discovery.WithPop(discovery.Track("Humble", "Tail Artist", discovery.ProviderSoundCloud, nil), 90),
			}},
			&discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{
				discovery.DeezerTrack("Humble", "Solid Artist", 10),
			}},
		}
	}
	if got, want := rankedArtists(t, providers()), []string{"Tail Artist", "Solid Artist"}; !slices.Equal(got, want) {
		t.Fatalf("without the option: %v, want %v (the more popular track first)", got, want)
	}
	if got, want := rankedArtists(t, providers(), discovery.WithTailDemotion()), []string{"Solid Artist", "Tail Artist"}; !slices.Equal(got, want) {
		t.Errorf("with tail demotion: %v, want %v", got, want)
	}
}

func TestWithCrossKindProminence_RanksTheMoreProminentTrackFirst(t *testing.T) {
	prominent := discovery.DeezerTrack("Humble", "Prominent Artist", 10)
	prominent.ProviderRank = 1000
	popular := discovery.DeezerTrack("Humble", "Popular Artist", 90)
	popular.ProviderRank = 1
	providers := func() []discovery.SearchProvider {
		return []discovery.SearchProvider{&discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{popular, prominent}}}
	}
	if got, want := rankedArtists(t, providers()), []string{"Popular Artist", "Prominent Artist"}; !slices.Equal(got, want) {
		t.Fatalf("without the option: %v, want %v (the more popular track first)", got, want)
	}
	if got, want := rankedArtists(t, providers(), discovery.WithCrossKindProminence()), []string{"Prominent Artist", "Popular Artist"}; !slices.Equal(got, want) {
		t.Errorf("with cross-kind prominence: %v, want %v", got, want)
	}
}

type driftingProvider struct {
	name   discovery.ProviderName
	slates [][]discovery.SearchResult
	calls  int
}

func (p *driftingProvider) Name() discovery.ProviderName { return p.name }

func (p *driftingProvider) Search(_ context.Context, _ string, _ map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
	slate := p.slates[min(p.calls, len(p.slates)-1)]
	p.calls++
	return slate, nil
}

func (p *driftingProvider) SupportedKinds() map[discovery.ResultKind]bool {
	return map[discovery.ResultKind]bool{discovery.ResultKindTrack: true}
}

func artistRun(title, prefix string, n int) []discovery.SearchResult {
	out := make([]discovery.SearchResult, n)
	for i := range out {
		out[i] = discovery.DeezerTrack(title, fmt.Sprintf("%s %02d", prefix, i), float64(90-i))
	}
	return out
}

func pagingService(t *testing.T, slates ...[]discovery.SearchResult) (*discovery.SearchService, *driftingProvider) {
	t.Helper()
	drifting := &driftingProvider{name: discovery.ProviderDeezer, slates: slates}
	down := &discovery.CountingProvider{Provider: discovery.ProviderITunes, Err: errors.New("upstream down")}
	svc := discovery.NewSearchService(
		[]discovery.SearchProvider{drifting, down},
		discovery.NewCircuitBreaker(),
		discovery.WithResultCache(discovery.NewFakeResultCache()),
	)
	return svc, drifting
}

func searchPage(
	t *testing.T,
	svc *discovery.SearchService,
	userId shared.UserId,
	raw string,
	offset, limit int,
	continues shared.EntityId,
) *discovery.SearchOutput {
	t.Helper()
	query, err := discovery.NewSearchQuery(raw, map[discovery.ResultKind]bool{discovery.ResultKindTrack: true}, limit)
	if err != nil {
		t.Fatal(err)
	}
	query.Offset = offset
	out, err := svc.ExecutePage(context.Background(), userId, query, false, continues)
	if err != nil {
		t.Fatalf("ExecutePage(offset=%d): %v", offset, err)
	}
	return out
}

func searchIdOf(t *testing.T, out *discovery.SearchOutput) shared.EntityId {
	t.Helper()
	id, err := shared.ParseEntityId(out.SearchId)
	if err != nil {
		t.Fatalf("SearchId %q is not a uuid: %v", out.SearchId, err)
	}
	return id
}

func TestService_PagesOfOneSearchComeFromOneRanking(t *testing.T) {
	svc, _ := pagingService(t, artistRun("Humble", "Alpha", 12), artistRun("Humble", "Bravo", 12))
	reference, _ := pagingService(t, artistRun("Humble", "Alpha", 12))
	userId := discovery.NewUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, shared.EntityId{})
	second := searchPage(t, svc, userId, "humble", 5, 5, searchIdOf(t, first))

	paged := append(discovery.Subtitles(first.Results), discovery.Subtitles(second.Results)...)
	unpaged := discovery.Subtitles(searchPage(t, reference, discovery.NewUser(), "humble", 0, 10, shared.EntityId{}).Results)
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
	userId := discovery.NewUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, shared.EntityId{})
	second := searchPage(t, svc, userId, "humble", 5, 5, shared.EntityId{})

	if drifting.calls != 2 {
		t.Errorf("provider calls = %d, want 2: a page naming no search fans out as before", drifting.calls)
	}
	if len(second.Results) != 5 {
		t.Fatalf("page two served %d results, want 5: %v", len(second.Results), discovery.Subtitles(second.Results))
	}
	if second.SearchId == first.SearchId {
		t.Error("a page cut from a fresh ranking must report a new search id")
	}
}

func TestService_SearchIdReplayedAgainstAnotherQueryServesThatQuery(t *testing.T) {
	svc, _ := pagingService(t, artistRun("Humble", "Alpha", 12), artistRun("Loyalty", "Bravo", 12))
	userId := discovery.NewUser()

	humble := searchPage(t, svc, userId, "humble", 0, 5, shared.EntityId{})
	loyalty := searchPage(t, svc, userId, "loyalty", 0, 5, searchIdOf(t, humble))

	if len(loyalty.Results) == 0 {
		t.Fatal("the borrowed id served nothing, so this proves nothing about which slate it read")
	}
	for _, artist := range discovery.Subtitles(loyalty.Results) {
		if !strings.HasPrefix(artist, "Bravo") {
			t.Fatalf("a search id replayed against another query served %v, want that query's own fan-out",
				discovery.Subtitles(loyalty.Results))
		}
	}
	if loyalty.SearchId == humble.SearchId {
		t.Error("a query that borrowed another search's id must report its own")
	}
}

func TestService_Execute_DoesNotLogQueryText(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const raw = "zqxj private diagnosis clinic"
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{
		discovery.Track("Clinic", "Someone", discovery.ProviderDeezer, nil),
	}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker())
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
	favorites := []discovery.Favorite{discovery.FavoriteOf(discovery.ResultKindArtist, "Kendrick Lamar", "")}
	results := []discovery.SearchResult{
		discovery.DeezerTrack("Humble", "Kendrick Lamar", 80),
		discovery.DeezerTrack("Humble Beginnings", "Kendrick Lamar", 70),
	}

	t.Run("real identity is personalized and recorded (control)", func(t *testing.T) {
		store, favs, history := runExecuteAs(t, discovery.NewUser(), favorites, results, true)
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
		store, favs, history := runExecuteAs(t, shared.SystemUserId(), favorites, results, true)
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
	user shared.UserId,
	favorites []discovery.Favorite,
	results []discovery.SearchResult,
	saveHistory bool,
) (*discovery.FakeEventStore, *discovery.FakeFavoritesRepo, []*discovery.SearchHistoryEntry) {
	t.Helper()
	store := &discovery.FakeEventStore{}
	favs := &discovery.FakeFavoritesRepo{Favorites: favorites}
	var recorded []*discovery.SearchHistoryEntry
	history := &discovery.FakeHistoryWriter{
		InsertFn: func(_ context.Context, entry *discovery.SearchHistoryEntry) error {
			recorded = append(recorded, entry)
			return nil
		},
	}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: results}
	svc := discovery.NewSearchService(
		[]discovery.SearchProvider{p},
		discovery.NewCircuitBreaker(),
		discovery.WithEventStore(store),
		discovery.WithFavorites(favs),
		discovery.WithHistoryRepository(history),
	)
	if _, err := svc.Execute(context.Background(), user, discovery.NewQuery(t, "humble"), saveHistory); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	svc.WaitForBackground()
	return store, favs, recorded
}

func TestMaybeExplore_DisabledIsInert(t *testing.T) {
	svc := discovery.NewSearchService(nil, discovery.NewCircuitBreaker())
	in := []discovery.SearchResult{{Title: "a"}, {Title: "b"}, {Title: "c"}}
	out, explored := svc.MaybeExplore(in)
	if explored {
		t.Error("exploration must be off when rate is 0")
	}
	if &out[0] != &in[0] {
		t.Error("inert path must return the same slice, not a copy")
	}
}

func TestIngestVocabulary_UsesOrganicOrderNotExploredShuffle(t *testing.T) {
	results := make([]discovery.SearchResult, 0, 20)
	for i := 0; i < 20; i++ {
		results = append(results, discovery.DeezerTrack("Humble Take "+string(rune('A'+i)), "Artist "+string(rune('A'+i)), float64(100-i)))
	}
	newSvc := func(store *discovery.FakeVocabularyStore, opts ...discovery.SearchOption) *discovery.SearchService {
		p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: results}
		opts = append([]discovery.SearchOption{discovery.WithVocabularyStore(store)}, opts...)
		return discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), opts...)
	}
	capture := func(store *discovery.FakeVocabularyStore) *[]string {
		var mu sync.Mutex
		terms := &[]string{}
		store.AddFn = func(e discovery.VocabularyEntry) error {
			mu.Lock()
			defer mu.Unlock()
			*terms = append(*terms, e.Term)
			return nil
		}
		return terms
	}

	for run := 0; run < 5; run++ {
		organicStore, exploredStore := &discovery.FakeVocabularyStore{}, &discovery.FakeVocabularyStore{}
		organicTerms := capture(organicStore)
		exploredTerms := capture(exploredStore)

		organicSvc := newSvc(organicStore)
		exploredSvc := newSvc(exploredStore, discovery.WithExploration(1.0))

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
	svc := discovery.NewSearchService(nil, discovery.NewCircuitBreaker(), discovery.WithExploration(1.0))
	in := []discovery.SearchResult{{Title: "a"}, {Title: "b"}, {Title: "c"}}
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

func expiringSlateService(t *testing.T, slates ...[]discovery.SearchResult) (*discovery.SearchService, *driftingProvider, *discovery.FakeResultCache) {
	t.Helper()
	drifting := &driftingProvider{name: discovery.ProviderDeezer, slates: slates}
	down := &discovery.CountingProvider{Provider: discovery.ProviderITunes, Err: errors.New("upstream down")}
	heldCache := discovery.NewFakeResultCache()
	svc := discovery.NewSearchService(
		[]discovery.SearchProvider{drifting, down},
		discovery.NewCircuitBreaker(),
		discovery.WithResultCache(discovery.NewFakeResultCache()),
		discovery.WithHeldSlateCache(heldCache),
	)
	return svc, drifting, heldCache
}

func TestService_PagingSurvivesAHeldSlateExpiring(t *testing.T) {
	svc, drifting, heldCache := expiringSlateService(
		t,
		artistRun("Humble", "Alpha", 12),
		artistRun("Humble", "Bravo", 12),
	)
	userId := discovery.NewUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, shared.EntityId{})
	if drifting.calls != 1 {
		t.Fatalf("provider calls after page one = %d, want 1", drifting.calls)
	}

	heldCache.Store = map[string][]discovery.SearchResult{}

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

	all := append(discovery.Subtitles(second.Results), discovery.Subtitles(third.Results)...)
	seen := map[string]bool{}
	for _, artist := range all {
		if seen[artist] {
			t.Fatalf("artist %q shown twice across pages two and three: %v", artist, all)
		}
		seen[artist] = true
	}
}

func TestService_SearchEmitsActivityWithoutQueryText(t *testing.T) {
	store := &discovery.FakeEventStore{}
	feed := &discovery.RecordingActivityFeed{}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Alright", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithEventStore(store), discovery.WithSearchActivityFeed(feed))

	runSearch(t, svc, "alright")
	svc.WaitForBackground()

	if got := feed.Recorded(); len(got) != 1 || got[0] != "search_performed" {
		t.Errorf("feed activity = %v, want [search_performed]", got)
	}
}

func TestService_ExecutePageReportsWhetherTheSlateCameFromCache(t *testing.T) {
	p := &discovery.CountingProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithResultCache(discovery.NewFakeResultCache()))
	userId := discovery.NewUser()

	fresh := searchPage(t, svc, userId, "humble", 0, 5, shared.EntityId{})
	if fresh.Cached {
		t.Error("a fresh fan-out reported Cached = true")
	}
	repeated := searchPage(t, svc, userId, "humble", 0, 5, shared.EntityId{})
	if !repeated.Cached {
		t.Error("a repeat answered from the result cache reported Cached = false")
	}
}

func TestService_ExecutePageReportsCachedForAHeldSlate(t *testing.T) {
	svc, _, _ := expiringSlateService(t, artistRun("Humble", "Alpha", 12))
	userId := discovery.NewUser()

	first := searchPage(t, svc, userId, "humble", 0, 5, shared.EntityId{})
	if first.Cached {
		t.Error("the first page reported Cached = true")
	}
	second := searchPage(t, svc, userId, "humble", 5, 5, searchIdOf(t, first))
	if !second.Cached {
		t.Error("a page cut from the held slate reported Cached = false")
	}
}

type gatedProvider struct {
	discovery.FakeProvider
	calls   atomic.Int32
	release chan struct{}
}

func (p *gatedProvider) Search(ctx context.Context, q string, k map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
	p.calls.Add(1)
	select {
	case <-p.release:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	return p.FakeProvider.Search(ctx, q, k)
}

func newCollapseService(release chan struct{}) (*discovery.SearchService, *gatedProvider) {
	p := &gatedProvider{
		FakeProvider: discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}},
		release:      release,
	}
	return discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker()), p
}

func TestService_Search_ColdStampedeSharesOneFanOut(t *testing.T) {
	release := make(chan struct{})
	svc, p := newCollapseService(release)
	const callers = 10
	outs := make([]*discovery.SearchOutput, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out, err := svc.Execute(context.Background(), discovery.NewUser(), discovery.NewQuery(t, "humble"), false)
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
	svc, _ := newCollapseService(release)
	cancelCtx, cancel := context.WithCancel(context.Background())
	done := make(chan *discovery.SearchOutput, 1)
	go func() {
		out, _ := svc.Execute(cancelCtx, discovery.NewUser(), discovery.NewQuery(t, "humble"), false)
		done <- out
	}()
	time.Sleep(50 * time.Millisecond)
	survivor := make(chan *discovery.SearchOutput, 1)
	go func() {
		out, _ := svc.Execute(context.Background(), discovery.NewUser(), discovery.NewQuery(t, "humble"), false)
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

type panickingBridge struct{}

func (panickingBridge) ExternalIDs(context.Context, discovery.ResultKind, string) (map[discovery.ProviderKey]string, bool) {
	panic("bridge bug")
}

func TestService_PanicBelowResolveColdDegradesToPartial(t *testing.T) {
	p := &discovery.FakeProvider{
		Provider: discovery.ProviderDeezer,
		Results:  []discovery.SearchResult{discovery.WithMBID(discovery.DeezerTrack("Humble", "Kendrick Lamar", 80), "mbid-1")},
		Delay:    150 * time.Millisecond,
	}
	cache := discovery.NewFakeResultCache()
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(),
		discovery.WithIdentityBridge(panickingBridge{}), discovery.WithResultCache(cache))

	const callers = 2
	outs := make([]*discovery.SearchOutput, callers)
	errs := make([]error, callers)
	var wg sync.WaitGroup
	for i := range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			outs[i], errs[i] = svc.Execute(context.Background(), discovery.NewUser(), discovery.NewQuery(t, "humble"), false)
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
	unpaged := discovery.Subtitles(searchPage(t, reference, discovery.NewUser(), "humble", 0, 50, shared.EntityId{}).Results)
	if len(unpaged) != total {
		t.Fatalf("one ranking holds %d results, want %d", len(unpaged), total)
	}
	userId := discovery.NewUser()
	first := searchPage(t, svc, userId, "humble", 0, limit, shared.EntityId{})
	id := searchIdOf(t, first)

	paged := discovery.Subtitles(first.Results)
	for offset := limit; offset <= total+limit; offset += limit {
		page := searchPage(t, svc, userId, "humble", offset, limit, id)
		if want := max(0, min(limit, total-offset)); len(page.Results) != want {
			t.Fatalf("page at offset %d served %d results, want %d: a short last page, then nothing", offset, len(page.Results), want)
		}
		paged = append(paged, discovery.Subtitles(page.Results)...)
	}

	if !slices.Equal(paged, unpaged) {
		t.Fatalf("pages = %v, want the one ranking %v", paged, unpaged)
	}
}

type stopObservingProvider struct {
	discovery.FakeProvider
	started chan struct{}
	stopped chan struct{}
}

func newStopObservingProvider() *stopObservingProvider {
	return &stopObservingProvider{
		FakeProvider: discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}},
		started:      make(chan struct{}, 8),
		stopped:      make(chan struct{}, 8),
	}
}

func (p *stopObservingProvider) Search(ctx context.Context, q string, k map[discovery.ResultKind]bool) ([]discovery.SearchResult, error) {
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
	p := newStopObservingProvider()
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = svc.Execute(ctx, discovery.NewUser(), discovery.NewQuery(t, "humble"), false)
		close(done)
	}()
	waitFor(t, p.started, "the provider call to start")

	cancel()

	waitFor(t, done, "the cancelled caller to return")
	waitFor(t, p.stopped, "the provider call to stop after its only caller left")
}

func TestService_Search_CallerAfterAbandonedSearchGetsAFreshAnswer(t *testing.T) {
	release := make(chan struct{})
	svc, p := newCollapseService(release)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		_, _ = svc.Execute(ctx, discovery.NewUser(), discovery.NewQuery(t, "humble"), false)
		close(done)
	}()
	time.Sleep(50 * time.Millisecond)
	cancel()
	waitFor(t, done, "the cancelled caller to return")

	close(release)
	out, err := svc.Execute(context.Background(), discovery.NewUser(), discovery.NewQuery(t, "humble"), false)
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
	buf := discovery.CaptureLogs(t)
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}, Delay: 30 * time.Millisecond}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker())

	runSearch(t, svc, "humble")

	var complete map[string]any
	if err := json.Unmarshal([]byte(discovery.LoggedEvent(t, buf, "search.v2.complete")), &complete); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"fanout_ms", "identity_ms", "rank_ms", "artwork_ms", "favorites_ms", "related_ms", "total_ms"} {
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
	resolver := &discovery.FakeArtworkResolver{URL: "https://art/new.jpg"}
	withArt := discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)
	withArt.ImageURL = "https://existing/art.jpg"
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{withArt}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithArtworkResolver(resolver))

	out := runSearch(t, svc, "humble")

	if out.Results[0].ImageURL != "https://existing/art.jpg" {
		t.Errorf("artwork = %q, want the existing image kept", out.Results[0].ImageURL)
	}
	if n := resolver.Calls(); n != 0 {
		t.Errorf("resolver called %d times, want 0 (track already had art)", n)
	}
}

func TestService_MBIDIndexAttachesMBIDForArtwork(t *testing.T) {
	resolver := &capturingArtworkResolver{url: "https://caa/hd.jpg"}
	idx := &fakeMBIDIndex{mbid: "warm-mbid"}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService(
		[]discovery.SearchProvider{p},
		discovery.NewCircuitBreaker(),
		discovery.WithArtworkResolver(resolver),
		discovery.WithArtworkCache(&discovery.FakeArtworkCache{Store: map[string]string{}}),
		discovery.WithMBIDIndex(idx),
	)

	runSearch(t, svc, "humble")
	svc.WaitForBackground()
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
	resolver := &capturingArtworkResolver{url: "https://caa/right-face.jpg"}
	store := &discovery.FakeIdentityStore{Mbid: "durable-mbid", Xref: map[discovery.ProviderKey]string{"discogs": "123"}}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService(
		[]discovery.SearchProvider{p},
		discovery.NewCircuitBreaker(),
		discovery.WithArtworkResolver(resolver),
		discovery.WithArtworkCache(&discovery.FakeArtworkCache{Store: map[string]string{}}),
		discovery.WithIdentityStore(store),
	)

	out := runSearch(t, svc, "humble")
	svc.WaitForBackground()

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
	resolver := &fakeIdentityAwareResolver{url: "https://caa/right.jpg"}
	cache := &discovery.FakeArtworkCache{Store: map[string]string{}}
	store := &discovery.FakeIdentityStore{Mbid: "durable-mbid", Xref: map[discovery.ProviderKey]string{"discogs": "123"}}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService(
		[]discovery.SearchProvider{p},
		discovery.NewCircuitBreaker(),
		discovery.WithArtworkResolver(resolver),
		discovery.WithArtworkCache(cache),
		discovery.WithIdentityStore(store),
	)

	runSearch(t, svc, "humble")
	svc.WaitForBackground()

	if got := cache.Store["Humble"]; got != "https://caa/right.jpg" {
		t.Errorf("cached artwork = %q, want the identity-resolved image", got)
	}
}

func TestService_ArtworkCacheShortCircuits(t *testing.T) {
	resolver := &discovery.FakeArtworkResolver{URL: "https://art/resolved.jpg"}
	cache := &discovery.FakeArtworkCache{Store: map[string]string{"Humble": "https://art/cached.jpg"}}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService(
		[]discovery.SearchProvider{p},
		discovery.NewCircuitBreaker(),
		discovery.WithArtworkResolver(resolver),
		discovery.WithArtworkCache(cache),
	)

	out := runSearch(t, svc, "humble")

	if out.Results[0].ImageURL != "https://art/cached.jpg" {
		t.Errorf("artwork = %q, want the cached image", out.Results[0].ImageURL)
	}
	if n := resolver.Calls(); n != 0 {
		t.Errorf("resolver called %d times, want 0 (cache hit)", n)
	}
}

func TestFillArtwork_PanickingResolverIsContained(t *testing.T) {
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithArtworkResolver(&panickingArtworkResolver{}))

	out := runSearch(t, svc, "humble")

	if len(out.Results) != 1 || out.Results[0].Title != "Humble" {
		t.Fatalf("results = %+v, want the original Humble result kept", out.Results)
	}
}

func TestService_MissingArtworkIsFetchedAfterTheAnswer(t *testing.T) {
	resolver := &gatedArtworkResolver{url: "https://caa/cover.jpg", release: make(chan struct{})}
	cache := &discovery.FakeArtworkCache{Store: map[string]string{}}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithArtworkResolver(resolver), discovery.WithArtworkCache(cache))

	first := runSearch(t, svc, "humble")

	if first.Results[0].ImageURL != "" {
		t.Fatalf("first answer ImageURL = %q, want none while the live lookup runs", first.Results[0].ImageURL)
	}
	if path := first.Results[0].ArtworkPath; path != discovery.ArtworkPathPending {
		t.Errorf("ArtworkPath = %q, want pending", path)
	}

	close(resolver.release)
	svc.WaitForBackground()

	second := runSearch(t, svc, "humble")
	if second.Results[0].ImageURL != "https://caa/cover.jpg" {
		t.Errorf("next answer ImageURL = %q, want the cover the background found", second.Results[0].ImageURL)
	}
}

func TestService_WithoutAnArtworkCacheNoLiveLookupRuns(t *testing.T) {
	resolver := &discovery.FakeArtworkResolver{URL: "https://art/cover.jpg"}
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithArtworkResolver(resolver))

	runSearch(t, svc, "humble")
	svc.WaitForBackground()

	if n := resolver.Calls(); n != 0 {
		t.Errorf("resolver called %d times, want 0 (nowhere to keep the answer)", n)
	}
}

type capturingArtworkResolver struct {
	url     string
	mu      sync.Mutex
	gotMBID string
}

func (r *capturingArtworkResolver) ResolveTagged(_ context.Context, _ discovery.ResultKind, _, _, mbid string) (string, discovery.ProviderKey, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if mbid != "" {
		r.gotMBID = mbid
	}
	return r.url, "", nil
}

func (r *capturingArtworkResolver) ResolveWithIdentityTagged(_ context.Context, _ discovery.ResultKind, _, _ string, _ discovery.ArtworkIdentity) (string, discovery.ProviderKey, error) {
	return "", "", nil
}

type fakeMBIDIndex struct {
	mbid    string
	lookups int32
}

func (f *fakeMBIDIndex) LookupMBID(_ context.Context, _ discovery.ResultKind, _ string) (string, bool) {
	atomic.AddInt32(&f.lookups, 1)
	if f.mbid == "" {
		return "", false
	}
	return f.mbid, true
}

func (f *fakeMBIDIndex) RememberMBID(_ context.Context, _ discovery.ResultKind, _, _ string) error {
	return nil
}

type fakeIdentityAwareResolver struct{ url string }

func (r *fakeIdentityAwareResolver) ResolveTagged(_ context.Context, _ discovery.ResultKind, _, _, _ string) (string, discovery.ProviderKey, error) {
	return "", "", nil
}

func (r *fakeIdentityAwareResolver) ResolveWithIdentityTagged(_ context.Context, _ discovery.ResultKind, _, _ string, id discovery.ArtworkIdentity) (string, discovery.ProviderKey, error) {
	if id.HasLinks() {
		return r.url, "", nil
	}
	return "", "", nil
}

type panickingArtworkResolver struct{ discovery.FakeArtworkResolver }

func (panickingArtworkResolver) ResolveTagged(context.Context, discovery.ResultKind, string, string, string) (string, discovery.ProviderKey, error) {
	panic("artwork resolver exploded")
}

type gatedArtworkResolver struct {
	url     string
	release chan struct{}
}

func (r *gatedArtworkResolver) ResolveTagged(ctx context.Context, _ discovery.ResultKind, _, _, _ string) (string, discovery.ProviderKey, error) {
	select {
	case <-r.release:
		return r.url, "caa", nil
	case <-ctx.Done():
		return "", "", ctx.Err()
	}
}

func (r *gatedArtworkResolver) ResolveWithIdentityTagged(_ context.Context, _ discovery.ResultKind, _, _ string, _ discovery.ArtworkIdentity) (string, discovery.ProviderKey, error) {
	return "", "", nil
}

func tripViaSearch(t *testing.T, cb *discovery.CircuitBreaker, provider discovery.ProviderName) {
	t.Helper()
	svc := discovery.NewSearchService([]discovery.SearchProvider{&discovery.FakeProvider{Provider: provider, Err: discovery.UpstreamDown}}, cb)
	for range breakerTripsAfter {
		inspect(t, svc, context.Background())
	}
	if cb.GetStatus(provider) != discovery.ProviderStatusCircuitOpen {
		t.Fatalf("setup: %s circuit not open after %d search failures", provider, breakerTripsAfter)
	}
}

func breakerTrack(provider discovery.ProviderName, id string) discovery.SearchResult {
	return discovery.TrackFrom(provider, id, "Humble", "Kendrick Lamar")
}

func TestContentBreaker_SearchTrippedProviderShortCircuitsContent(t *testing.T) {
	cases := []struct {
		name     string
		provider discovery.ProviderName
		run      func(cb *discovery.CircuitBreaker, calls *atomic.Int32) (*discovery.ContentFetchResponse, error)
	}{
		{
			name:     "top tracks",
			provider: discovery.ProviderDeezer,
			run: func(cb *discovery.CircuitBreaker, calls *atomic.Int32) (*discovery.ContentFetchResponse, error) {
				p := &discovery.FakeArtistContentProvider{GetTopTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
					calls.Add(1)
					return []discovery.SearchResult{breakerTrack(discovery.ProviderDeezer, "1")}, nil
				}}
				svc := discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: p},
					discovery.WithContentCircuitBreaker(cb))
				return svc.GetTopTracks(context.Background(), discovery.ProviderDeezer, "42", nil, 10)
			},
		},
		{
			name:     "albums",
			provider: discovery.ProviderDeezer,
			run: func(cb *discovery.CircuitBreaker, calls *atomic.Int32) (*discovery.ContentFetchResponse, error) {
				p := &discovery.FakeArtistContentProvider{GetAlbumsFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
					calls.Add(1)
					return []discovery.SearchResult{discovery.V2Album(discovery.ProviderDeezer, "a", "DAMN.")}, nil
				}}
				svc := discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: p},
					discovery.WithContentCircuitBreaker(cb))
				return svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "42", nil, 10)
			},
		},
		{
			name:     "identity fan-out (v2) albums",
			provider: discovery.ProviderDeezer,
			run: func(cb *discovery.CircuitBreaker, calls *atomic.Int32) (*discovery.ContentFetchResponse, error) {
				p := &discovery.FakeArtistContentProvider{GetAlbumsFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
					calls.Add(1)
					return []discovery.SearchResult{discovery.V2Album(discovery.ProviderDeezer, "a", "DAMN.")}, nil
				}}
				svc := discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: p},
					discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{}), discovery.WithContentCircuitBreaker(cb))
				return svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "42", nil, 10)
			},
		},
		{
			name:     "album tracks with deezer search fallback",
			provider: discovery.ProviderDeezer,
			run: func(cb *discovery.CircuitBreaker, calls *atomic.Int32) (*discovery.ContentFetchResponse, error) {
				p := &discovery.FakeAlbumContentProvider{GetAlbumTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
					calls.Add(1)
					return []discovery.SearchResult{breakerTrack(discovery.ProviderDeezer, "1")}, nil
				}}
				searcher := &discovery.CountingProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{discovery.AlbumSearchResult("Kendrick Lamar", "9")}}
				svc := discovery.NewGetAlbumTracksService(map[discovery.ProviderName]discovery.AlbumContentProvider{discovery.ProviderDeezer: p},
					discovery.WithAlbumFallbackSearcher(searcher), discovery.WithAlbumCircuitBreaker(cb))
				resp, err := svc.ExecuteRequest(context.Background(), discovery.AlbumTracksRequest{
					Provider: discovery.ProviderDeezer, ExternalID: "7", Title: "DAMN.", Artist: "Kendrick Lamar",
				})
				calls.Add(int32(searcher.Calls))
				return resp, err
			},
		},
		{
			name:     "related tracks",
			provider: discovery.ProviderSoundCloud,
			run: func(cb *discovery.CircuitBreaker, calls *atomic.Int32) (*discovery.ContentFetchResponse, error) {
				p := &countingRelatedProvider{calls: calls}
				svc := discovery.NewGetRelatedTracksService(map[string]discovery.RelatedTracksProvider{"soundcloud": p},
					discovery.WithRelatedCircuitBreaker(cb))
				return svc.Execute(context.Background(), discovery.ProviderSoundCloud, "7", 10)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := discovery.NewCircuitBreaker()
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
	cb := discovery.NewCircuitBreaker()
	tripViaSearch(t, cb, discovery.ProviderDeezer)
	svc := discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{
		discovery.ProviderDeezer: &discovery.FakeArtistContentProvider{},
	}, discovery.WithContentCircuitBreaker(cb))

	resp, err := svc.GetTopTracks(context.Background(), discovery.ProviderDeezer, "42", nil, 10)
	if err != nil {
		t.Fatalf("error = %v", err)
	}
	if resp.Status != discovery.ProviderStatusCircuitOpen {
		t.Errorf("status = %v, want circuit_open", resp.Status)
	}
}

func TestContentBreaker_ContentFailuresTripSearch(t *testing.T) {
	cb := discovery.NewCircuitBreaker()
	var contentCalls atomic.Int32
	p := &discovery.FakeArtistContentProvider{GetTopTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
		contentCalls.Add(1)
		return nil, discovery.UpstreamDown
	}}
	content := discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: p},
		discovery.WithContentCircuitBreaker(cb))
	for range breakerTripsAfter + 2 {
		if _, err := content.GetTopTracks(context.Background(), discovery.ProviderDeezer, "42", nil, 10); err != nil {
			t.Fatalf("error = %v", err)
		}
	}
	if n := contentCalls.Load(); n != breakerTripsAfter {
		t.Errorf("content calls = %d, want %d (calls after the circuit opened are skipped)", n, breakerTripsAfter)
	}

	searcher := &discovery.CountingProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{breakerTrack(discovery.ProviderDeezer, "1")}}
	_, statuses := inspect(t, discovery.NewSearchService([]discovery.SearchProvider{searcher}, cb), context.Background())
	if searcher.Calls != 0 || statuses[0].Status != discovery.ProviderStatusCircuitOpen {
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
		{"transport error", discovery.UpstreamDown, true},
		{"provider timeout", fmt.Errorf("fetch: %w", context.DeadlineExceeded), true},
		{"5xx", fmt.Errorf("musicbrainz status 503: %w", discovery.StatusErr(503)), true},
		{"429", discovery.StatusErr(429), true},
		{"404 for an unknown id", discovery.StatusErr(404), false},
		{"provider data error for a bogus id", errors.New("deezer api error 800 (DataException): no data"), false},
		{"canceled", context.Canceled, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := discovery.NewCircuitBreaker()
			p := &discovery.FakeRelatedProvider{Err: tc.err}
			svc := discovery.NewGetRelatedTracksService(map[string]discovery.RelatedTracksProvider{"soundcloud": p},
				discovery.WithRelatedCircuitBreaker(cb))
			for range breakerTripsAfter * 2 {
				_, _ = svc.Execute(context.Background(), discovery.ProviderSoundCloud, "bogus", 10)
			}
			if open := cb.GetStatus(discovery.ProviderSoundCloud) == discovery.ProviderStatusCircuitOpen; open != tc.wantOpen {
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
		wantState discovery.CircuitState
	}{
		{name: "success closes", wantState: discovery.CircuitClosed},
		{name: "failure reopens", err: discovery.UpstreamDown, wantState: discovery.CircuitOpen},
		{name: "caller cancel releases", cancel: true, err: discovery.UpstreamDown, wantState: discovery.CircuitHalfOpen},
		{name: "request-scoped error releases", err: discovery.StatusErr(404), wantState: discovery.CircuitHalfOpen},
		{name: "panic reopens", panics: true, wantState: discovery.CircuitOpen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := discovery.NewCircuitBreaker()
			tripToHalfOpenWindow(t, cb, discovery.ProviderDeezer)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			p := &discovery.FakeArtistContentProvider{GetAlbumsFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
				if tc.cancel {
					cancel()
				}
				if tc.panics {
					panic("boom")
				}
				return []discovery.SearchResult{discovery.V2Album(discovery.ProviderDeezer, "a", "DAMN.")}, tc.err
			}}
			svc := discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: p},
				discovery.WithContentIdentityStore(&discovery.FakeIdentityStore{}), discovery.WithContentCircuitBreaker(cb))

			_, _ = svc.GetAlbums(ctx, discovery.ProviderDeezer, "42", nil, 10)

			entry := cb.Snapshot(discovery.ProviderDeezer)
			if entry.State != tc.wantState {
				t.Errorf("state = %v, want %v", entry.State, tc.wantState)
			}
			if entry.State == discovery.CircuitHalfOpen && !cb.AllowRequest(context.Background(), discovery.ProviderDeezer) {
				t.Error("probe slot still held: next request not admitted")
			}
		})
	}
}

type countingRelatedProvider struct {
	calls *atomic.Int32
}

func (p *countingRelatedProvider) GetRelatedTracks(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
	p.calls.Add(1)
	return []discovery.SearchResult{breakerTrack(discovery.ProviderSoundCloud, "1")}, nil
}

type persistentAuthErr struct{ discovery.StatusErr }

func (persistentAuthErr) PersistentAuthFailure() bool { return true }

type badPayloadErr struct{ error }

func (badPayloadErr) BadPayload() bool { return true }

func TestContentBreaker_PersistentAuthAndBadPayloadTripBreaker(t *testing.T) {
	cases := []struct {
		name     string
		err      error
		wantOpen bool
	}{
		{"persistent auth failure", fmt.Errorf("top tracks: %w", persistentAuthErr{401}), true},
		{"bad payload on a 200", badPayloadErr{errors.New("invalid character '<'")}, true},
		{"bare 401", discovery.StatusErr(401), false},
		{"bare 403", discovery.StatusErr(403), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cb := discovery.NewCircuitBreaker()
			p := &discovery.FakeArtistContentProvider{GetTopTracksFn: func(context.Context, discovery.ProviderName, string) ([]discovery.SearchResult, error) {
				return nil, tc.err
			}}
			content := discovery.NewGetArtistContentService(map[discovery.ProviderName]discovery.ArtistContentProvider{discovery.ProviderDeezer: p},
				discovery.WithContentCircuitBreaker(cb))
			for range breakerTripsAfter {
				_, _ = content.GetTopTracks(context.Background(), discovery.ProviderDeezer, "42", nil, 10)
			}
			if open := cb.GetStatus(discovery.ProviderDeezer) == discovery.ProviderStatusCircuitOpen; open != tc.wantOpen {
				t.Errorf("circuit open = %v, want %v", open, tc.wantOpen)
			}
		})
	}
}

func libraryTitles(out *discovery.SearchOutput) []string {
	var titles []string
	for _, group := range out.Related {
		if group.Relationship != discovery.RelationshipLibraryMatches {
			continue
		}
		for _, item := range group.Items {
			titles = append(titles, item.Title)
		}
	}
	return titles
}

func searchAs(t *testing.T, svc *discovery.SearchService, userId shared.UserId, raw string) *discovery.SearchOutput {
	t.Helper()
	out, err := svc.Execute(context.Background(), userId, discovery.NewQuery(t, raw), false)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	return out
}

func TestService_RepeatedSearch_ServesRelatedGroupsWithoutRefetching(t *testing.T) {
	organic, tracksByAlbum := relatedSearchFixture()
	querier := &discovery.CountingRelationshipQuerier{}
	albumProvider := &discovery.CountingAlbumTracksProvider{TracksByAlbum: tracksByAlbum}
	svc := discovery.NewSearchService(
		[]discovery.SearchProvider{&discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: organic}},
		discovery.NewCircuitBreaker(),
		discovery.WithResultCache(discovery.NewFakeResultCache()),
		discovery.WithFindRelatedService(discovery.NewFindRelatedService(querier, albumProvider, nil)),
	)
	user := discovery.NewUser()

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
	organic, tracksByAlbum := relatedSearchFixture()
	owner, other := discovery.NewUser(), discovery.NewUser()
	querier := &discovery.CountingRelationshipQuerier{
		MatchesByUser: map[shared.UserId][]discovery.RelatedTrackMatch{
			owner: {{Title: "Owner Library Track", Artist: "Artist 0", Album: "Album 0"}},
			other: {{Title: "Other Library Track", Artist: "Artist 0", Album: "Album 0"}},
		},
	}
	svc := discovery.NewSearchService(
		[]discovery.SearchProvider{&discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: organic}},
		discovery.NewCircuitBreaker(),
		discovery.WithResultCache(discovery.NewFakeResultCache()),
		discovery.WithFindRelatedService(discovery.NewFindRelatedService(querier, &discovery.CountingAlbumTracksProvider{TracksByAlbum: tracksByAlbum}, nil)),
	)

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
	owner shared.UserId
	match discovery.RelatedTrackMatch
}

type ownershipQuerier struct {
	rows []ownedTrackRow
}

func (q *ownershipQuerier) FindRelatedByAlbumArtist(_ context.Context, userId shared.UserId, album string, _ string, _ int) ([]discovery.RelatedTrackMatch, error) {
	var out []discovery.RelatedTrackMatch
	for _, r := range q.rows {
		if r.owner == userId && r.match.Album == album {
			out = append(out, r.match)
		}
	}
	return out, nil
}

func TestSearch_RelatedLibraryMatches_NeverLeakAnotherUsersLibrary(t *testing.T) {
	userA := shared.NewUserId(uuid.New())
	userB := shared.NewUserId(uuid.New())
	const album = "Shared Album"

	querier := &ownershipQuerier{rows: []ownedTrackRow{
		{owner: userA, match: discovery.RelatedTrackMatch{Title: "A Private Song", Artist: "Band", Album: album}},
		{owner: userB, match: discovery.RelatedTrackMatch{Title: "B Own Song", Artist: "Band", Album: album}},
	}}

	organic := discovery.DeezerTrack("Hit Single", "Band", 80)
	organic.Album = album
	p := &discovery.FakeProvider{Provider: discovery.ProviderDeezer, Results: []discovery.SearchResult{organic}}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(),
		discovery.WithFindRelatedService(discovery.NewFindRelatedService(querier, nil, nil)))

	out, err := svc.Execute(context.Background(), userB, discovery.NewQuery(t, "hit single"), false)
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

func relatedSearchFixture() ([]discovery.SearchResult, map[string][]discovery.SearchResult) {
	var organic []discovery.SearchResult
	tracksByAlbum := map[string][]discovery.SearchResult{}
	for i := range relatedSubjects {
		albumID := fmt.Sprintf("al-%d", i)
		track := discovery.TrackResult(discovery.ProviderDeezer, fmt.Sprintf("d-%d", i),
			fmt.Sprintf("Humble %d", i), fmt.Sprintf("Artist %d", i), nil)
		track.Album = fmt.Sprintf("Album %d", i)
		track.DeezerAlbumID = albumID
		organic = append(organic, track)
		tracksByAlbum[albumID] = []discovery.SearchResult{
			discovery.TrackResult(discovery.ProviderDeezer, fmt.Sprintf("s-%d", i),
				fmt.Sprintf("Sibling %d", i), fmt.Sprintf("Artist %d", i), nil),
		}
	}
	return organic, tracksByAlbum
}

func TestSearch_RawQueryIsNeverSuggestedToAnotherUser(t *testing.T) {
	const private = "jane doe 42 elm street"
	vocab := &sharedVocabulary{}
	store := vocab.store()
	p := &discovery.QueryFakeProvider{
		Provider: discovery.ProviderDeezer,
		ResultsByQuery: map[string][]discovery.SearchResult{
			private: {discovery.DeezerTrack("Elm Street", "Jane Band", 60)},
		},
	}
	svc := discovery.NewSearchService([]discovery.SearchProvider{p}, discovery.NewCircuitBreaker(), discovery.WithVocabularyStore(store))

	runSearch(t, svc, private)
	svc.WaitForBackground()

	suggest := discovery.NewSuggestService(store)
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
	entries []discovery.VocabularyEntry
}

func (v *sharedVocabulary) store() *discovery.FakeVocabularyStore {
	return &discovery.FakeVocabularyStore{
		AddFn: func(e discovery.VocabularyEntry) error {
			v.mu.Lock()
			defer v.mu.Unlock()
			v.entries = append(v.entries, e)
			return nil
		},
		SuggestByPrefixFn: func(prefix string, limit int) ([]discovery.VocabularyEntry, error) {
			v.mu.Lock()
			defer v.mu.Unlock()
			var matches []discovery.VocabularyEntry
			for _, e := range v.entries {
				if strings.HasPrefix(e.TermNorm, prefix) && len(matches) < limit {
					matches = append(matches, e)
				}
			}
			return matches, nil
		},
	}
}

func suggestedNorms(t *testing.T, suggest *discovery.SuggestService, partial string) []string {
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
