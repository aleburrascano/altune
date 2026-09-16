package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"context"
	"testing"
	"time"
)

// seedEvent appends one event of the given type at the given time through the real
// Append, so a retention test exercises the same rows the write path produces.
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

// countEventsOfType returns how many rows of one event type remain in the table,
// so a prune's per-type effect can be asserted directly rather than inferred.
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

// TestPgxEventStore_PruneEvents_PerTypeWindow proves the retention invariant for
// every non-discography event type against real Postgres: a row inside the type's
// window and a row exactly at the retention cutoff both survive, while a row past
// the window is evicted. It is table-driven off eventRetention itself, so a type
// added to the policy without a window — or a window narrowed below its read
// window — is caught here rather than in production as silent data loss.
func TestPgxEventStore_PruneEvents_PerTypeWindow(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	now := time.Now().UTC()

	for _, ret := range eventRetention {
		t.Run(ret.eventType.String(), func(t *testing.T) {
			deleteEventsOfType(t, store, ret.eventType)
			t.Cleanup(func() { deleteEventsOfType(t, store, ret.eventType) })

			// fresh is well inside the window; boundary sits exactly at the cutoff
			// (a strict `<` must keep it); ancient is one day past the window.
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

// TestPgxEventStore_PruneEvents_LeavesDiscographyObserved proves PruneEvents never
// touches discography_observed — that type owns the wider discographyRetentionWindow
// and its own eviction, so folding it into the per-type prune would evict cases the
// discography endpoint reads over its 365-day window.
func TestPgxEventStore_PruneEvents_LeavesDiscographyObserved(t *testing.T) {
	pool := testPool(t)
	store := NewPgxEventStore(pool)
	t.Cleanup(func() { deleteEventsOfType(t, store, domain.EventTypeDiscographyObserved) })
	deleteEventsOfType(t, store, domain.EventTypeDiscographyObserved)

	now := time.Now().UTC()
	// Older than every per-type window but inside discography's own window.
	seedObservation(t, store, now.Add(-aggregateEventRetention-30*24*time.Hour), "spotify:kept", 10, 8,
		map[string]int{"spotify": 10})

	if _, err := store.PruneEvents(context.Background(), now); err != nil {
		t.Fatalf("PruneEvents: %v", err)
	}
	if got := countEventsOfType(t, store, domain.EventTypeDiscographyObserved); got != 1 {
		t.Fatalf("discography_observed rows after PruneEvents = %d, want 1 (untouched)", got)
	}
}
