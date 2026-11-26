package searchtest

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/service/useractivity"
	"altune/go-api/internal/shared/clock"
	"testing"
)

type RecordingActivityFeed = memory.RecordingActivityFeed

func NewRecordEvent(t testing.TB, store ports.EventStore) *useractivity.RecordEventService {
	t.Helper()
	return NewRecordEventWithFeed(t, store, useractivity.NoopActivityFeed{})
}

func NewRecordEventWithFeed(t testing.TB, store ports.EventStore, feed ports.ActivityFeed) *useractivity.RecordEventService {
	t.Helper()
	svc, err := useractivity.NewRecordEventService(store, feed, clock.Now)
	if err != nil {
		t.Fatalf("useractivity.NewRecordEventService: %v", err)
	}
	return svc
}
