package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"testing"
)

type RecordingActivityFeed = memory.RecordingActivityFeed

func NewRecordEvent(t testing.TB, store ports.EventStore) *service.RecordEventService {
	t.Helper()
	return NewRecordEventWithFeed(t, store, service.NoopActivityFeed{})
}

func NewRecordEventWithFeed(t testing.TB, store ports.EventStore, feed ports.ActivityFeed) *service.RecordEventService {
	t.Helper()
	svc, err := service.NewRecordEventService(store, feed, clock.Now)
	if err != nil {
		t.Fatalf("useractivity.NewRecordEventService: %v", err)
	}
	return svc
}
