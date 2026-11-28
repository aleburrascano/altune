package persistence_test

import (
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

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

func adoptStoredOwners(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO auth.users (id)
		SELECT user_id FROM discovery_search_history
		UNION SELECT user_id FROM discovery_favorites
		UNION SELECT user_id FROM discovery_events
		ON CONFLICT DO NOTHING`); err != nil {
		t.Fatalf("adopt stored owners: %v", err)
	}
}

func forgetIdentity(t *testing.T, pool *pgxpool.Pool, owner ids.UserId) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM auth.users WHERE id = $1`, owner.UUID()); err != nil {
		t.Fatalf("forget identity: %v", err)
	}
}

const seededQuery = "mac demarco"

type storedRowsOf struct {
	history   int
	favorites int
	events    int
}

func countStoredRows(t *testing.T, pool *pgxpool.Pool, owner ids.UserId) storedRowsOf {
	t.Helper()
	return storedRowsOf{
		history:   countSeededRows(t, pool, `SELECT COUNT(*) FROM discovery_search_history WHERE user_id = $1 AND query_norm = $2`, owner),
		favorites: countSeededRows(t, pool, `SELECT COUNT(*) FROM discovery_favorites WHERE user_id = $1 AND entity_key = $2`, owner),
		events:    countSeededRows(t, pool, `SELECT COUNT(*) FROM discovery_events WHERE user_id = $1 AND query_norm = $2`, owner),
	}
}

func countSeededRows(t *testing.T, pool *pgxpool.Pool, countSQL string, owner ids.UserId) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), countSQL, owner.UUID(), seededQuery).Scan(&n); err != nil {
		t.Fatalf("count seeded rows of %s: %v", owner.UUID(), err)
	}
	return n
}

func countOwnedRows(t *testing.T, pool *pgxpool.Pool, countSQL string, owner ids.UserId) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), countSQL, owner.UUID()).Scan(&n); err != nil {
		t.Fatalf("count rows of %s: %v", owner.UUID(), err)
	}
	return n
}

func seedEveryDiscoveryTableFor(t *testing.T, pool *pgxpool.Pool, owner ids.UserId) {
	t.Helper()
	ctx := context.Background()
	t.Cleanup(func() { deleteStoredRowsOf(t, pool, owner) })

	history := persistence.NewPostgresSearchHistoryRepository(pool)
	if err := history.Insert(ctx, &domain.SearchHistoryEntry{
		ID:         ids.NewEntityId(),
		UserId:     owner,
		Query:      "Mac DeMarco",
		QueryNorm:  seededQuery,
		ExecutedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed search history: %v", err)
	}

	favorites := persistence.NewPostgresFavoritesRepository(pool)
	if err := favorites.Add(ctx, owner, domain.Favorite{
		Kind:  domain.ResultKindArtist,
		Key:   seededQuery,
		Title: "Mac DeMarco",
	}); err != nil {
		t.Fatalf("seed favorite: %v", err)
	}

	events := persistence.NewPostgresEventStore(pool, clock.Now)
	if err := events.Append(ctx, domain.InteractionEvent{
		OccurredAt: time.Now().UTC(),
		UserId:     owner,
		Type:       domain.InteractionTypeSearchPerformed,
		QueryNorm:  seededQuery,
		Payload:    map[string]any{},
	}); err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

func deleteStoredRowsOf(t *testing.T, pool *pgxpool.Pool, owner ids.UserId) {
	t.Helper()
	for _, deleteSQL := range []string{
		`DELETE FROM discovery_search_history WHERE user_id = $1 AND query_norm = $2`,
		`DELETE FROM discovery_favorites WHERE user_id = $1 AND entity_key = $2`,
		`DELETE FROM discovery_events WHERE user_id = $1 AND query_norm = $2`,
	} {
		if _, err := pool.Exec(context.Background(), deleteSQL, owner.UUID(), seededQuery); err != nil {
			t.Fatalf("clean up rows of %s: %v", owner.UUID(), err)
		}
	}
}

func deletedIdentityErasers(pool *pgxpool.Pool) []ports.DeletedUserDataEraser {
	return []ports.DeletedUserDataEraser{
		persistence.NewPostgresSearchHistoryRepository(pool),
		persistence.NewPostgresFavoritesRepository(pool),
		persistence.NewPostgresEventStore(pool, clock.Now),
	}
}

func sweepDeletedIdentities(t *testing.T, pool *pgxpool.Pool) int64 {
	t.Helper()
	var erased int64
	for _, eraser := range deletedIdentityErasers(pool) {
		rows, err := eraser.EraseRowsOfDeletedIdentities(context.Background())
		if err != nil {
			t.Fatalf("EraseRowsOfDeletedIdentities: %v", err)
		}
		erased += rows
	}
	return erased
}

func TestDeletedIdentitySweep_ErasesAGoneAccountAndKeepsALiveOne(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	standUpIdentityStore(t, pool)
	deleted := ids.NewUserId(uuid.New())
	stillRegistered := ids.NewUserId(uuid.New())
	seedEveryDiscoveryTableFor(t, pool, deleted)
	seedEveryDiscoveryTableFor(t, pool, stillRegistered)
	adoptStoredOwners(t, pool)
	forgetIdentity(t, pool, deleted)

	erased := sweepDeletedIdentities(t, pool)

	if erased < 3 {
		t.Errorf("sweep erased %d rows, want at least 3 (one per discovery table)", erased)
	}
	if got := countStoredRows(t, pool, deleted); got != (storedRowsOf{}) {
		t.Errorf("the deleted account still has %+v stored, want every table empty", got)
	}
	want := storedRowsOf{history: 1, favorites: 1, events: 1}
	if got := countStoredRows(t, pool, stillRegistered); got != want {
		t.Errorf("the live account has %+v stored, want %+v (untouched)", got, want)
	}
}

func TestDeletedIdentitySweep_KeepsTheSystemIdentitysServerEmittedRows(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	standUpIdentityStore(t, pool)
	system := ids.SystemUserId()
	deleted := ids.NewUserId(uuid.New())
	seedEveryDiscoveryTableFor(t, pool, system)
	seedEveryDiscoveryTableFor(t, pool, ids.NewUserId(uuid.New()))
	seedEveryDiscoveryTableFor(t, pool, deleted)
	adoptStoredOwners(t, pool)
	forgetIdentity(t, pool, system)
	forgetIdentity(t, pool, deleted)

	erased := sweepDeletedIdentities(t, pool)

	if erased != 3 {
		t.Fatalf("sweep erased %d rows, want 3 (the deleted account's, and only those)", erased)
	}
	want := storedRowsOf{history: 1, favorites: 1, events: 1}
	if got := countStoredRows(t, pool, system); got != want {
		t.Errorf("system identity rows = %+v, want %+v (never a deleted account)", got, want)
	}
}

func TestDeletedIdentitySweep_AnEmptyIdentityStoreErasesNothing(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	standUpIdentityStore(t, pool)
	owner := ids.NewUserId(uuid.New())
	seedEveryDiscoveryTableFor(t, pool, owner)

	erased := sweepDeletedIdentities(t, pool)

	if erased != 0 {
		t.Errorf("sweep erased %d rows against an identity store showing no identities, want 0", erased)
	}
	want := storedRowsOf{history: 1, favorites: 1, events: 1}
	if got := countStoredRows(t, pool, owner); got != want {
		t.Errorf("stored rows = %+v, want %+v (nothing erased)", got, want)
	}
}

func TestDeletedIdentitySweep_AnUnreadableIdentityStoreIsNotEveryAccountDeleted(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	var existing *string
	if err := pool.QueryRow(context.Background(),
		`SELECT to_regclass('auth.users')::text`).Scan(&existing); err != nil {
		t.Fatalf("look up the identity store: %v", err)
	}
	if existing != nil {
		t.Skip("auth.users exists here; the unreadable case cannot be reached")
	}
	owner := ids.NewUserId(uuid.New())
	seedEveryDiscoveryTableFor(t, pool, owner)

	for _, eraser := range deletedIdentityErasers(pool) {
		erased, err := eraser.EraseRowsOfDeletedIdentities(context.Background())
		if !errors.Is(err, domain.ErrIdentityStoreUnavailable) {
			t.Errorf("error = %v, want one satisfying ErrIdentityStoreUnavailable", err)
		}
		if erased != 0 {
			t.Errorf("erased = %d with no identity store to read, want 0", erased)
		}
	}
	want := storedRowsOf{history: 1, favorites: 1, events: 1}
	if got := countStoredRows(t, pool, owner); got != want {
		t.Errorf("stored rows = %+v, want %+v (nothing erased)", got, want)
	}
}

func TestMigrations_NoTableKeepsPerUserSearchClicks(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)

	var found *string
	if err := pool.QueryRow(context.Background(),
		`SELECT to_regclass('discovery_search_clicks')::text`).Scan(&found); err != nil {
		t.Fatalf("look up discovery_search_clicks: %v", err)
	}
	if found != nil {
		t.Errorf("discovery_search_clicks still exists as %q; no writer and no erase path covers it", *found)
	}
}
