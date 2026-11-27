package persistence_test

import (
	"altune/go-api/internal/platform/monitoring/internal/adapters/persistence"
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newOwner() ids.UserId { return ids.NewUserId(uuid.New()) }

func countClientEvents(t *testing.T, pool *pgxpool.Pool, owner ids.UserId) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT COUNT(*) FROM client_events WHERE user_id = $1`, owner.UUID()).Scan(&n); err != nil {
		t.Fatalf("count client events: %v", err)
	}
	return n
}

func TestPostgresClientEventStore_AppendStoresTheEventAndIgnoresARepeatedEventId(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresClientEventStore(pool)
	owner := newOwner()
	event := domain.ClientEvent{
		UserId:           owner,
		Type:             domain.ClientEventClientError,
		EventId:          uuid.NewString(),
		CorrelationId:    "corr-1",
		ClientOccurredAt: time.Date(2026, 11, 27, 10, 0, 0, 0, time.UTC),
		OccurredAt:       time.Date(2026, 11, 27, 10, 0, 1, 0, time.UTC),
		Payload:          map[string]any{"message": "boom"},
	}

	for range 2 {
		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	if got := countClientEvents(t, pool, owner); got != 1 {
		t.Fatalf("rows after a repeated event id = %d, want 1", got)
	}
	var kind, correlation, message string
	if err := pool.QueryRow(ctx,
		`SELECT event_type, correlation_id, payload->>'message' FROM client_events WHERE user_id = $1`, owner.UUID(),
	).Scan(&kind, &correlation, &message); err != nil {
		t.Fatalf("read the row back: %v", err)
	}
	if kind != "client_error" || correlation != "corr-1" || message != "boom" {
		t.Errorf("row = (%q, %q, %q), want (client_error, corr-1, boom)", kind, correlation, message)
	}
}

func TestPostgresClientEventStore_EventsWithoutAnIdAreAllKept(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	store := persistence.NewPostgresClientEventStore(pool)
	owner := newOwner()
	event := domain.ClientEvent{UserId: owner, Type: domain.ClientEventScreenView, OccurredAt: time.Now()}

	for range 3 {
		if err := store.Append(ctx, event); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	if got := countClientEvents(t, pool, owner); got != 3 {
		t.Errorf("rows = %d, want 3: events with no event id are never deduplicated", got)
	}
}

func TestPostgresClientEventStore_ErasesOnlyTheRowsOfDeletedIdentities(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Pool(t)
	standUpIdentityStore(t, pool)
	store := persistence.NewPostgresClientEventStore(pool)
	kept, gone := newOwner(), newOwner()
	for _, owner := range []ids.UserId{kept, gone} {
		if err := store.Append(ctx, domain.ClientEvent{UserId: owner, Type: domain.ClientEventScreenView, OccurredAt: time.Now()}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if _, err := pool.Exec(ctx, `INSERT INTO auth.users (id) VALUES ($1)`, kept.UUID()); err != nil {
		t.Fatalf("register the surviving identity: %v", err)
	}

	erased, err := store.EraseRowsOfDeletedIdentities(ctx)
	if err != nil {
		t.Fatalf("EraseRowsOfDeletedIdentities: %v", err)
	}

	if countClientEvents(t, pool, gone) != 0 || countClientEvents(t, pool, kept) != 1 {
		t.Errorf("rows kept=%d gone=%d, want 1 and 0", countClientEvents(t, pool, kept), countClientEvents(t, pool, gone))
	}
	if erased < 1 {
		t.Errorf("erased = %d, want at least the deleted identity's row", erased)
	}
}

func standUpIdentityStore(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	var existing *string
	if err := pool.QueryRow(ctx, `SELECT to_regclass('auth.users')::text`).Scan(&existing); err != nil {
		t.Fatalf("look up the identity store: %v", err)
	}
	if existing != nil {
		t.Skip("auth.users is the real identity store here; this test will not write to it")
	}
	if _, err := pool.Exec(ctx, `CREATE SCHEMA auth; CREATE TABLE auth.users (id UUID PRIMARY KEY);
		CREATE VIEW public.auth_user_ids AS SELECT id FROM auth.users`); err != nil {
		t.Fatalf("create the identity store: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DROP SCHEMA IF EXISTS auth CASCADE`)
	})
}
