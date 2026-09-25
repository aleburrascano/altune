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

type recordingAdminActivity struct {
	mu     sync.Mutex
	events []string
}

func (r *recordingAdminActivity) Emit(eventType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, eventType)
}

func (r *recordingAdminActivity) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

var _ ports.AdminActivity = (*recordingAdminActivity)(nil)

// TestRecordEventService_EmitsAdminActivityOnRecordedEvent pins #2594: a
// recorded play/skip/library_add event surfaces its type on the admin feed, so
// Overseer's usage bucket has something other than the discovery_events table
// to read.
func TestRecordEventService_EmitsAdminActivityOnRecordedEvent(t *testing.T) {
	store := &fakeEventStore{}
	admin := &recordingAdminActivity{}
	svc := NewRecordEventService(store, WithRecordEventAdminActivity(admin))

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

// TestRecordEventService_SkipsAdminActivityOnStoreFailure proves a failed
// append never reports activity that was not actually recorded.
func TestRecordEventService_SkipsAdminActivityOnStoreFailure(t *testing.T) {
	admin := &recordingAdminActivity{}
	svc := NewRecordEventService(failingEventStore{}, WithRecordEventAdminActivity(admin))

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

// TestService_SearchEmitsAdminActivityWithoutQueryText pins #2594: a completed
// search surfaces "search_performed" on the admin feed, carrying nothing the
// admin.Emit seam does not already forbid by signature (no user id, no query).
func TestService_SearchEmitsAdminActivityWithoutQueryText(t *testing.T) {
	store := &fakeEventStore{}
	admin := &recordingAdminActivity{}
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{deezerTrack("Alright", "Kendrick Lamar", 80)}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker(), WithEventStore(store), WithSearchAdminActivity(admin))

	runSearch(t, svc, "alright")
	svc.WaitForBackground()

	if got := admin.recorded(); len(got) != 1 || got[0] != "search_performed" {
		t.Errorf("admin activity = %v, want [search_performed]", got)
	}
}
