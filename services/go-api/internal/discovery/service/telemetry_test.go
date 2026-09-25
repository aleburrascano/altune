package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
)

type fakeEventStore struct {
	mu     sync.Mutex
	events []domain.InteractionEvent
	err    error
}

func (f *fakeEventStore) Append(_ context.Context, e domain.InteractionEvent) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.events = append(f.events, e)
	return nil
}

func (f *fakeEventStore) snapshot() []domain.InteractionEvent {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]domain.InteractionEvent(nil), f.events...)
}

func (f *fakeEventStore) recorded() []domain.InteractionEvent {
	return f.snapshot()
}

func TestService_EmitsSearchTelemetryV2(t *testing.T) {
	store := &fakeEventStore{}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithEventStore(store))

	runSearch(t, svc, "humble")
	svc.WaitForBackground()

	events := store.snapshot()
	if len(events) != 1 {
		t.Fatalf("got %d events, want 1", len(events))
	}
	e := events[0]
	if e.Type != domain.EventTypeSearchPerformed {
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

func TestService_TelemetryFailureDoesNotSurface(t *testing.T) {
	store := &fakeEventStore{err: errors.New("db down")}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithEventStore(store))

	out, err := svc.Execute(context.Background(), newUser(), newQuery(t, "humble"), false)
	svc.WaitForBackground()

	if err != nil {
		t.Fatalf("Execute returned error %v; telemetry failure must not surface", err)
	}
	if out == nil || len(out.Results) != 1 {
		t.Fatalf("search result unaffected by telemetry failure, got %v", out)
	}
}

func TestService_NoEventStoreNoEmit(t *testing.T) {
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker())
	runSearch(t, svc, "humble")
	svc.WaitForBackground()
}

// #573: the aggregation only trusts signatures the server proves it showed,
// so search_performed must carry every signature the response can surface —
// including results beyond the first page, which later pages never re-emit.
func TestService_SearchTelemetryRecordsShownSignatures(t *testing.T) {
	store := &fakeEventStore{}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{
		deezerTrack("Humble", "Kendrick Lamar", 80),
		deezerTrack("Humble Pie", "Other Artist", 60),
		deezerTrack("Humble Beginnings", "Third Artist", 40),
	}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithEventStore(store))
	q, err := domain.NewSearchQuery("humble", map[domain.ResultKind]bool{domain.ResultKindTrack: true}, 1)
	if err != nil {
		t.Fatal(err)
	}

	out, err := svc.Execute(context.Background(), newUser(), q, false)
	svc.WaitForBackground()
	if err != nil {
		t.Fatal(err)
	}

	events := store.snapshot()
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
	surfaced := append([]domain.SearchResult(nil), out.Results...)
	for _, sec := range out.Slate.Sections {
		surfaced = append(surfaced, sec.Items...)
	}
	for _, r := range surfaced {
		sig := r.Signature
		if sig == "" {
			sig = domain.ResultSignature(r)
		}
		if !set[sig] {
			t.Errorf("surfaced result %q signature %q missing from shown_signatures %v", r.Title, sig, shown)
		}
	}
}

// Regression test for #2244: a detached job's failure line must carry what an
// operator needs to diagnose it without a reproduction.
func TestSearchTelemetry_DroppedEventLogsSearchAndUser(t *testing.T) {
	store := &fakeEventStore{err: errors.New("db down")}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithEventStore(store))
	user := newUser()
	ring := captureDetachedLogs(t)

	out, err := svc.Execute(context.Background(), user, newQuery(t, "humble"), false)
	svc.WaitForBackground()
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec := onlyRecord(t, ring, "search.v2.telemetry_emit_failed")
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
