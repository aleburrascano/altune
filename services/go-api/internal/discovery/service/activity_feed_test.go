package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"context"
	"sync"
	"testing"

	"github.com/google/uuid"
)

type recordingActivityFeed struct {
	mu     sync.Mutex
	events []string
}

func (r *recordingActivityFeed) EmitActivity(eventType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, eventType)
}

func (r *recordingActivityFeed) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

var _ ports.ActivityFeed = (*recordingActivityFeed)(nil)

// Overseer's usage bucket has something other than the discovery_events table
// to read.
func TestRecordEventService_EmitsActivityOnRecordedEvent(t *testing.T) {
	store := &fakeEventStore{}
	admin := &recordingActivityFeed{}
	svc := NewRecordEventService(store, WithRecordEventActivityFeed(admin))

	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type:    domain.EventTypeLibraryAdd,
		EventId: uuid.New().String(),
		Payload: map[string]any{"track_id": "abc"},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if got := admin.recorded(); len(got) != 1 || got[0] != "library_add" {
		t.Errorf("admin activity = %v, want [library_add]", got)
	}
}

// append never reports activity that was not actually recorded.
func TestRecordEventService_SkipsActivityOnStoreFailure(t *testing.T) {
	admin := &recordingActivityFeed{}
	svc := NewRecordEventService(failingEventStore{}, WithRecordEventActivityFeed(admin))

	err := svc.Execute(context.Background(), shared.NewUserId(uuid.New()), RecordEventInput{
		Type: domain.EventTypePlay,
	})
	if err == nil {
		t.Fatal("Execute: want error from a failing store")
	}
	if got := admin.recorded(); len(got) != 0 {
		t.Errorf("admin activity = %v, want none", got)
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
