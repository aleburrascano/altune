package query_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/searchtest"
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestService_EmitsSearchTelemetryV2(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &search.FakeEventStore{}
	p := &search.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{search.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), search.WithEventStore(store))

	runSearch(t, svc, "humble")
	bg.Wait()

	events := store.Snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != search.EventTypeSearchPerformed {
		t.Errorf("type = %v, want search_performed", e.Type)
	}
	if e.Payload["pipeline_version"] != "v2" {
		t.Errorf("pipeline_version = %v, want v2", e.Payload["pipeline_version"])
	}
	if e.Payload["result_count"] != 1 {
		t.Errorf("result_count = %v, want 1", e.Payload["result_count"])
	}
	if e.Payload["zero_result"] != false {
		t.Errorf("zero_result = %v, want false", e.Payload["zero_result"])
	}
	if _, ok := e.Payload["top"]; !ok {
		t.Error("payload missing top")
	}
	if e.SearchId == "" {
		t.Error("search_performed event missing the minted search_id keystone")
	}
}

func TestService_SearchTelemetryCarriesStageTimes(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &search.FakeEventStore{}
	p := &search.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{search.DeezerTrack("Humble", "Kendrick Lamar", 80)}, Delay: 30 * time.Millisecond}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), search.WithEventStore(store))

	runSearch(t, svc, "humble")
	bg.Wait()

	payload := store.Snapshot()[0].Payload
	ms := map[string]int64{}
	for _, key := range []string{"fanout_ms", "rank_ms", "artwork_ms", "favorites_ms", "related_ms", "total_ms"} {
		v, ok := payload[key].(int64)
		if !ok {
			t.Fatalf("payload[%q] = %#v, want milliseconds", key, payload[key])
		}
		ms[key] = v
	}
	if ms["fanout_ms"] < 30 || ms["total_ms"] < ms["fanout_ms"] {
		t.Errorf("fanout_ms = %d, total_ms = %d, want fan-out at least the 30ms provider wait and total at least that", ms["fanout_ms"], ms["total_ms"])
	}
	if payload["cached"] != false || payload["partial"] != false {
		t.Errorf("cached = %v, partial = %v, want false for a fresh search every provider answered", payload["cached"], payload["partial"])
	}
}

func TestService_TelemetryFailureDoesNotSurface(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &search.FakeEventStore{Err: errors.New("db down")}
	p := &search.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{search.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), search.WithEventStore(store))

	out, err := svc.Execute(context.Background(), search.NewUser(), search.NewQuery(t, "humble"), false)
	bg.Wait()

	if err != nil {
		t.Fatalf("Execute returned error %v; telemetry failure must not surface", err)
	}
	if out == nil || len(out.Results) != 1 {
		t.Fatalf("search result unaffected by telemetry failure, got %v", out)
	}
}

func TestService_NoEventStoreNoEmit(t *testing.T) {
	bg := searchtest.NewTracker(t)
	p := &search.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{search.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker())
	runSearch(t, svc, "humble")
	bg.Wait()
}

func TestService_SearchTelemetryRecordsShownSignatures(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &search.FakeEventStore{}
	p := &search.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{
		search.DeezerTrack("Humble", "Kendrick Lamar", 80),
		search.DeezerTrack("Humble Pie", "Other Artist", 60),
		search.DeezerTrack("Humble Beginnings", "Third Artist", 40),
	}}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), search.WithEventStore(store))
	q, err := search.NewSearchQuery("humble", map[search.ResultKind]bool{search.ResultKindTrack: true}, 1)
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.Execute(context.Background(), search.NewUser(), q, false)
	bg.Wait()
	if err != nil {
		t.Fatal(err)
	}

	events := store.Snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	shown, ok := events[0].Payload["shown_signatures"].([]string)
	if !ok {
		t.Fatalf("shown_signatures = %T, want []string", events[0].Payload["shown_signatures"])
	}
	if len(out.Results) != 1 || len(shown) != out.Total {
		t.Errorf("len(shown_signatures) = %d, page = %d, want %d (the whole slate, not just the page)", len(shown), len(out.Results), out.Total)
	}
	set := map[string]bool{}
	for _, s := range shown {
		set[s] = true
	}
	surfaced := append([]search.SearchResult(nil), out.Results...)
	for _, sec := range out.Slate.Sections {
		surfaced = append(surfaced, sec.Items...)
	}
	for _, r := range surfaced {
		if r.Signature == "" {
			t.Errorf("surfaced result %q carries no signature", r.Title)
			continue
		}
		if !set[r.Signature] {
			t.Errorf("surfaced result %q signature %q missing from shown_signatures %v", r.Title, r.Signature, shown)
		}
	}
}

func TestSearchTelemetry_DroppedEventLogsSearchAndUser(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &search.FakeEventStore{Err: errors.New("db down")}
	p := &search.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{search.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := search.NewSearchService(bg, []search.SearchProvider{p}, search.NewCircuitBreaker(), search.WithEventStore(store))
	user := search.NewUser()
	ring := search.CaptureDetachedLogs(t)

	out, err := svc.Execute(context.Background(), user, search.NewQuery(t, "humble"), false)
	bg.Wait()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec := search.OnlyRecord(t, ring, "search.v2.telemetry_emit_failed")
	if rec.Attrs["search_id"] != out.SearchId {
		t.Errorf("search_id = %q, want %q: the dropped event is unfindable without it", rec.Attrs["search_id"], out.SearchId)
	}
	if rec.Attrs["user_id"] != user.String() {
		t.Errorf("user_id = %q, want %q", rec.Attrs["user_id"], user.String())
	}
	if !strings.Contains(rec.Attrs["error"], "db down") {
		t.Errorf("error = %q, want the store's failure", rec.Attrs["error"])
	}
}
