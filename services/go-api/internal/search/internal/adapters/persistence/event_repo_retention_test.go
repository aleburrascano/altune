package persistence_test

import (
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/testdb"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
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

func newEventOwner(t *testing.T, pool *pgxpool.Pool) ids.UserId {
	t.Helper()
	owner := ids.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM discovery_events WHERE user_id = $1`, owner.UUID())
	})
	return owner
}

func seedEvent(t *testing.T, store *persistence.PostgresEventStore, owner ids.UserId, at time.Time, eventType domain.EventType) {
	t.Helper()
	if err := store.Append(context.Background(), domain.InteractionEvent{
		OccurredAt: at,
		UserId:     owner,
		Type:       eventType,
		Payload:    map[string]any{},
	}); err != nil {
		t.Fatalf("append %s: %v", eventType, err)
	}
}

func countEventsOfType(t *testing.T, pool *pgxpool.Pool, owner ids.UserId, eventType domain.EventType) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(),
		`SELECT COUNT(*) FROM discovery_events WHERE user_id = $1 AND event_type = $2`,
		owner.UUID(), eventType.String()).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", eventType, err)
	}
	return n
}

func deleteOwnEvents(t *testing.T, pool *pgxpool.Pool, owner ids.UserId) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM discovery_events WHERE user_id = $1`, owner.UUID()); err != nil {
		t.Fatalf("clean own events: %v", err)
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
			owner := newEventOwner(t, pool)

			seedEvent(t, store, owner, now.Add(-1*time.Hour), ret.eventType)
			seedEvent(t, store, owner, now.Add(-ret.window), ret.eventType)
			seedEvent(t, store, owner, now.Add(-ret.window-24*time.Hour), ret.eventType)

			pruned, err := store.PruneEvents(context.Background(), now)
			if err != nil {
				t.Fatalf("PruneEvents: %v", err)
			}
			if pruned < 1 {
				t.Fatalf("pruned = %d, want >= 1 (the row past %s's window)", pruned, ret.eventType)
			}
			if got := countEventsOfType(t, pool, owner, ret.eventType); got != 2 {
				t.Fatalf("%s rows after prune = %d, want 2 (in-window + boundary survive, ancient evicted)",
					ret.eventType, got)
			}
		})
	}
}

func TestPostgresEventStore_PruneEvents_LeavesDiscographyObserved(t *testing.T) {
	pool := testdb.Pool(t)
	store := persistence.NewPostgresEventStore(pool)
	testdb.ExclusiveDiscographyObservations(t, pool)
	owner := newEventOwner(t, pool)

	now := time.Now().UTC()
	seedEvent(t, store, owner, now.Add(-persistence.AggregateEventRetention-30*24*time.Hour), domain.EventTypeDiscographyObserved)
	seedEvent(t, store, newEventOwner(t, pool), now.Add(-time.Hour), domain.EventTypeDiscographyObserved)

	if _, err := store.PruneEvents(context.Background(), now); err != nil {
		t.Fatalf("PruneEvents: %v", err)
	}
	if got := countEventsOfType(t, pool, owner, domain.EventTypeDiscographyObserved); got != 1 {
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
	owner := newEventOwner(t, pool)
	clean := func() { deleteOwnEvents(t, pool, owner) }

	day := 24 * time.Hour
	ages := []time.Duration{
		time.Hour, 3 * day, 6 * day, 8 * day, 13 * day, 15 * day, 29 * day, 31 * day,
		45 * day, 61 * day, 89 * day, 91 * day, 181 * day, 366 * day, 800 * day,
	}
	for _, age := range ages {
		t.Run(age.String(), func(t *testing.T) {
			clean()
			for _, et := range types {
				seedEvent(t, store, owner, now.Add(-age), et)
			}

			if _, err := store.PruneEvents(context.Background(), now); err != nil {
				t.Fatalf("PruneEvents: %v", err)
			}
			health := countEventsOfType(t, pool, owner, domain.EventTypePlaybackHealth)
			for _, et := range types[1:] {
				if got := countEventsOfType(t, pool, owner, et); got != health {
					t.Errorf("%s rows aged %s after prune = %d, want %d (same as playback_health)",
						et, age, got, health)
				}
			}
		})
	}

	t.Run("very old rows are evicted", func(t *testing.T) {
		clean()
		for _, et := range types[1:] {
			seedEvent(t, store, owner, now.Add(-800*day), et)
		}
		if _, err := store.PruneEvents(context.Background(), now); err != nil {
			t.Fatalf("PruneEvents: %v", err)
		}
		for _, et := range types[1:] {
			if got := countEventsOfType(t, pool, owner, et); got != 0 {
				t.Errorf("%s rows aged 800d after prune = %d, want 0", et, got)
			}
		}
	})
}
