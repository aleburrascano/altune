package persistence_test

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/testdb"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const writeOnlyEventRetention = 30 * 24 * time.Hour

var retentionPolicy = []struct {
	eventType domain.EventType
	window    time.Duration
}{
	{domain.EventTypeSearchPerformed, persistence.AggregateEventRetention},
	{domain.EventTypeResultClicked, persistence.AggregateEventRetention},
	{domain.EventTypePlay, persistence.AggregateEventRetention},
	{domain.EventTypeSkip, persistence.AggregateEventRetention},
	{domain.EventTypeCompleted, persistence.AggregateEventRetention},
	{domain.EventTypeLibraryAdd, persistence.AggregateEventRetention},
	{domain.EventTypeWrongAlbum, persistence.AggregateEventRetention},
	{domain.EventTypeResultsShown, writeOnlyEventRetention},
	{domain.EventTypeSearchFailed, writeOnlyEventRetention},
	{domain.EventTypeSearchDegraded, writeOnlyEventRetention},
	{domain.EventTypePlaybackHealth, writeOnlyEventRetention},
	{domain.EventTypeDetailHealth, writeOnlyEventRetention},
	{domain.EventTypeAcquisitionUi, writeOnlyEventRetention},
	{domain.EventTypeClientError, writeOnlyEventRetention},
	{domain.EventTypeUserAction, writeOnlyEventRetention},
	{domain.EventTypeFailureShown, writeOnlyEventRetention},
	{domain.EventTypeSseReconnect, writeOnlyEventRetention},
	{domain.EventTypeOutboxFlushFailed, writeOnlyEventRetention},
	{domain.EventTypeDownloadFailed, writeOnlyEventRetention},
	{domain.EventTypeAuthFailed, writeOnlyEventRetention},
	{domain.EventTypeScreenView, writeOnlyEventRetention},
	{domain.EventTypePlayStart, writeOnlyEventRetention},
}

func seedEvent(t *testing.T, store *persistence.PostgresEventStore, at time.Time, eventType domain.EventType) {
	t.Helper()
	if err := store.Append(context.Background(), domain.InteractionEvent{
		OccurredAt: at,
		UserId:     ids.SystemUserId(),
		Type:       eventType,
		Payload:    map[string]any{},
	}); err != nil {
		t.Fatalf("append %s: %v", eventType, err)
	}
}

func countEventsOfType(t *testing.T, pool *pgxpool.Pool, eventType domain.EventType) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM discovery_events WHERE event_type = $1`,
		eventType.String()).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", eventType, err)
	}
	return n
}

func deleteEventsOfType(t *testing.T, pool *pgxpool.Pool, eventType domain.EventType) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM discovery_events WHERE event_type = $1`, eventType.String()); err != nil {
		t.Fatalf("clean %s: %v", eventType, err)
	}
}

func TestRetentionPolicy_NamesEveryEventTypeButDiscographyObserved(t *testing.T) {
	listed := map[domain.EventType]bool{}
	for _, ret := range retentionPolicy {
		listed[ret.eventType] = true
	}
	for et := domain.EventTypeUnknown + 1; et.String() != "unknown"; et++ {
		if et != domain.EventTypeDiscographyObserved && !listed[et] {
			t.Errorf("event type %s has no retention window in this test's policy table", et)
		}
	}
}

func TestPostgresEventStore_PruneEvents_PerTypeWindow(t *testing.T) {
	pool := testdb.Pool(t)
	store := persistence.NewPostgresEventStore(pool)
	now := time.Now().UTC()

	for _, ret := range retentionPolicy {
		t.Run(ret.eventType.String(), func(t *testing.T) {
			deleteEventsOfType(t, pool, ret.eventType)
			t.Cleanup(func() { deleteEventsOfType(t, pool, ret.eventType) })

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
			if got := countEventsOfType(t, pool, ret.eventType); got != 2 {
				t.Fatalf("%s rows after prune = %d, want 2 (in-window + boundary survive, ancient evicted)",
					ret.eventType, got)
			}
		})
	}
}

func TestPostgresEventStore_PruneEvents_LeavesDiscographyObserved(t *testing.T) {
	pool := testdb.Pool(t)
	store := persistence.NewPostgresEventStore(pool)
	t.Cleanup(func() { deleteEventsOfType(t, pool, domain.EventTypeDiscographyObserved) })
	deleteEventsOfType(t, pool, domain.EventTypeDiscographyObserved)

	now := time.Now().UTC()
	seedObservation(t, store, now.Add(-persistence.AggregateEventRetention-30*24*time.Hour), "spotify:kept", 10, 8,
		map[string]int{"spotify": 10})

	if _, err := store.PruneEvents(context.Background(), now); err != nil {
		t.Fatalf("PruneEvents: %v", err)
	}
	if got := countEventsOfType(t, pool, domain.EventTypeDiscographyObserved); got != 1 {
		t.Fatalf("discography_observed rows after PruneEvents = %d, want 1 (untouched)", got)
	}
}

func TestPostgresEventStore_PruneEvents_UserTelemetryFollowsHealthRetention(t *testing.T) {
	pool := testdb.Pool(t)
	store := persistence.NewPostgresEventStore(pool)
	now := time.Now().UTC()
	types := []domain.EventType{
		domain.EventTypePlaybackHealth, domain.EventTypeUserAction, domain.EventTypeFailureShown,
		domain.EventTypeSseReconnect, domain.EventTypeOutboxFlushFailed, domain.EventTypeDownloadFailed,
	}
	clean := func() {
		for _, et := range types {
			deleteEventsOfType(t, pool, et)
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
			health := countEventsOfType(t, pool, domain.EventTypePlaybackHealth)
			for _, et := range types[1:] {
				if got := countEventsOfType(t, pool, et); got != health {
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
			if got := countEventsOfType(t, pool, et); got != 0 {
				t.Errorf("%s rows aged 800d after prune = %d, want 0", et, got)
			}
		}
	})
}
