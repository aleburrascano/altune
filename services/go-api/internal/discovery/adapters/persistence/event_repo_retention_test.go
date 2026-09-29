package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"context"
	"testing"
	"time"
)

func seedEvent(t *testing.T, store *PgxEventStore, at time.Time, eventType domain.EventType) {
	t.Helper()
	if err := store.Append(context.Background(), domain.InteractionEvent{
		OccurredAt: at,
		UserId:     shared.SystemUserId(),
		Type:       eventType,
		Payload:    map[string]any{},
	}); err != nil {
		t.Fatalf("append %s: %v", eventType, err)
	}
}

func countEventsOfType(t *testing.T, store *PgxEventStore, eventType domain.EventType) int {
	t.Helper()
	var n int
	if err := store.pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM discovery_events WHERE event_type = $1`,
		eventType.String()).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", eventType, err)
	}
	return n
}

func deleteEventsOfType(t *testing.T, store *PgxEventStore, eventType domain.EventType) {
	t.Helper()
	if _, err := store.pool.Exec(context.Background(),
		`DELETE FROM discovery_events WHERE event_type = $1`, eventType.String()); err != nil {
		t.Fatalf("clean %s: %v", eventType, err)
	}
}

func TestPgxEventStore_PruneEvents_PerTypeWindow(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	now := time.Now().UTC()

	for _, ret := range eventRetention {
		t.Run(ret.eventType.String(), func(t *testing.T) {
			deleteEventsOfType(t, store, ret.eventType)
			t.Cleanup(func() { deleteEventsOfType(t, store, ret.eventType) })

			seedEvent(t, store, now.Add(-1*time.Hour), ret.eventType)
			seedEvent(t, store, now.Add(-ret.window), ret.eventType)
			seedEvent(t, store, now.Add(-ret.window-24*time.Hour), ret.eventType)

			pruned, err := store.PruneEvents(context.Background(), now)
			if err != nil {
				t.Fatalf("PruneEvents: %v", err)
			}
			if pruned < 1 {
				t.Fatalf("pruned = %d, want >= 1 (the row past %s's window)", pruned, ret.eventType)
			}
			if got := countEventsOfType(t, store, ret.eventType); got != 2 {
				t.Fatalf("%s rows after prune = %d, want 2 (in-window + boundary survive, ancient evicted)",
					ret.eventType, got)
			}
		})
	}
}

func TestPgxEventStore_PruneEvents_LeavesDiscographyObserved(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	t.Cleanup(func() { deleteEventsOfType(t, store, domain.EventTypeDiscographyObserved) })
	deleteEventsOfType(t, store, domain.EventTypeDiscographyObserved)

	now := time.Now().UTC()
	seedObservation(t, store, now.Add(-aggregateEventRetention-30*24*time.Hour), "spotify:kept", 10, 8,
		map[string]int{"spotify": 10})

	if _, err := store.PruneEvents(context.Background(), now); err != nil {
		t.Fatalf("PruneEvents: %v", err)
	}
	if got := countEventsOfType(t, store, domain.EventTypeDiscographyObserved); got != 1 {
		t.Fatalf("discography_observed rows after PruneEvents = %d, want 1 (untouched)", got)
	}
}

func TestPgxEventStore_PruneEvents_UserTelemetryFollowsHealthRetention(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	now := time.Now().UTC()
	types := []domain.EventType{
		domain.EventTypePlaybackHealth, domain.EventTypeUserAction, domain.EventTypeFailureShown,
	}
	clean := func() {
		for _, et := range types {
			deleteEventsOfType(t, store, et)
		}
	}
	t.Cleanup(clean)

	day := 24 * time.Hour
	ages := []time.Duration{
		time.Hour, 3 * day, 6 * day, 8 * day, 13 * day, 15 * day, 29 * day, 31 * day,
		45 * day, 61 * day, 89 * day, 91 * day, 181 * day, 366 * day, 800 * day,
	}
	for _, age := range ages {
		t.Run(age.String(), func(t *testing.T) {
			clean()
			for _, et := range types {
				seedEvent(t, store, now.Add(-age), et)
			}

			if _, err := store.PruneEvents(context.Background(), now); err != nil {
				t.Fatalf("PruneEvents: %v", err)
			}
			health := countEventsOfType(t, store, domain.EventTypePlaybackHealth)
			for _, et := range types[1:] {
				if got := countEventsOfType(t, store, et); got != health {
					t.Errorf("%s rows aged %s after prune = %d, want %d (same as playback_health)",
						et, age, got, health)
				}
			}
		})
	}

	t.Run("very old rows are evicted", func(t *testing.T) {
		clean()
		for _, et := range types[1:] {
			seedEvent(t, store, now.Add(-800*day), et)
		}
		if _, err := store.PruneEvents(context.Background(), now); err != nil {
			t.Fatalf("PruneEvents: %v", err)
		}
		for _, et := range types[1:] {
			if got := countEventsOfType(t, store, et); got != 0 {
				t.Errorf("%s rows aged 800d after prune = %d, want 0", et, got)
			}
		}
	})
}
