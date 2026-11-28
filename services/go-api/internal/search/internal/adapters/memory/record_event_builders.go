package memory

import (
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
)

func NewRecordEvent(t TB, store ports.EventStore) *service.RecordEventService {
	t.Helper()
	return NewRecordEventWithFeed(t, store, service.NoopActivityFeed{})
}

func NewRecordEventWithFeed(t TB, store ports.EventStore, feed ports.ActivityFeed) *service.RecordEventService {
	t.Helper()
	svc, err := service.NewRecordEventService(store, feed, clock.Now)
	if err != nil {
		t.Fatalf("useractivity.NewRecordEventService: %v", err)
	}
	return svc
}
