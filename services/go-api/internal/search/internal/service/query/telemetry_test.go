package query_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/searchtest"
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestService_EmitsSearchTelemetryV2(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &memory.FakeEventStore{}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Events = store })

	runSearch(t, svc, "humble")
	bg.Wait()

	events := store.Snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != domain.InteractionTypeSearchPerformed {
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
	store := &memory.FakeEventStore{}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}, Delay: 30 * time.Millisecond}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Events = store })

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
	store := &memory.FakeEventStore{Err: errors.New("db down")}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Events = store })

	out, err := svc.Execute(context.Background(), memory.NewUser(), memory.NewQuery(t, "humble"))
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
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchService(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now))
	runSearch(t, svc, "humble")
	bg.Wait()
}

func TestService_SearchTelemetryRecordsShownSignatures(t *testing.T) {
	bg := searchtest.NewTracker(t)
	store := &memory.FakeEventStore{}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{
		memory.DeezerTrack("Humble", "Kendrick Lamar", 80),
		memory.DeezerTrack("Humble Pie", "Other Artist", 60),
		memory.DeezerTrack("Humble Beginnings", "Third Artist", 40),
	}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Events = store })
	q, err := search.NewSearchQuery("humble", map[search.ResultKind]bool{search.ResultKindTrack: true}, 1)
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.Execute(context.Background(), memory.NewUser(), q)
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
	store := &memory.FakeEventStore{Err: errors.New("db down")}
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(c *search.Collaborators) { c.Events = store })
	user := memory.NewUser()
	ring := memory.CaptureDetachedLogs(t)

	out, err := svc.Execute(context.Background(), user, memory.NewQuery(t, "humble"))
	bg.Wait()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec := memory.OnlyRecord(t, ring, "search.v2.telemetry_emit_failed")
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

func TestService_SearchRecordsAStageSpanAndAProviderSpanUnderIt(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	previous := otel.GetTracerProvider()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder)))
	t.Cleanup(func() { otel.SetTracerProvider(previous) })
	bg := searchtest.NewTracker(t)
	p := &memory.FakeProvider{Provider: search.ProviderDeezer, Results: []search.SearchResult{memory.DeezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := searchtest.NewSearchServiceWith(t, bg, []search.SearchProvider{p}, search.NewCircuitBreaker(clock.Now), func(*search.Collaborators) {})

	runSearch(t, svc, "humble")
	bg.Wait()

	spans := map[string]sdktrace.ReadOnlySpan{}
	for _, s := range recorder.Ended() {
		spans[s.Name()] = s
	}
	fanout, provider := spans["search.fanout"], spans["search.provider"]
	if fanout == nil || provider == nil || spans["search.rank"] == nil || spans["search.artwork"] == nil {
		t.Fatalf("spans = %v, want the fanout, provider, rank and artwork stages", spans)
	}
	if provider.Parent().SpanID() != fanout.SpanContext().SpanID() {
		t.Errorf("provider span parent = %v, want the fanout span", provider.Parent().SpanID())
	}
	attrs := map[string]string{}
	for _, kv := range provider.Attributes() {
		attrs[string(kv.Key)] = kv.Value.String()
	}
	if attrs["provider"] == "" || attrs["provider.status"] != "ok" {
		t.Errorf("provider span attributes = %v, want the provider and an ok status", attrs)
	}
}
