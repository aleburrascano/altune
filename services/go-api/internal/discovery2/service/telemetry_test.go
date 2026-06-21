package service

import (
	"context"
	"errors"
	"sync"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	legacy "altune/go-api/internal/discovery/service"
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

func TestService_EmitsSearchTelemetryV2(t *testing.T) {
	store := &fakeEventStore{}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, legacy.NewCircuitBreaker(), WithEventStore(store))

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
}

func TestService_TelemetryFailureDoesNotSurface(t *testing.T) {
	store := &fakeEventStore{err: errors.New("db down")}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, legacy.NewCircuitBreaker(), WithEventStore(store))

	out, err := svc.Execute(context.Background(), newUser(), newQuery(t, "humble"), false)
	svc.WaitForBackground() // must not panic despite the append error

	if err != nil {
		t.Fatalf("Execute returned error %v; telemetry failure must not surface", err)
	}
	if out == nil || len(out.Results) != 1 {
		t.Fatalf("search result unaffected by telemetry failure, got %v", out)
	}
}

func TestService_NoEventStoreNoEmit(t *testing.T) {
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Humble", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, legacy.NewCircuitBreaker())
	runSearch(t, svc, "humble")
	svc.WaitForBackground() // no-op, must not block or panic
}
