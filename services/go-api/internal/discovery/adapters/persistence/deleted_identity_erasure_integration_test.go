package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/sharedtest"
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
	if _, err := pool.Exec(ctx, `CREATE SCHEMA auth; CREATE TABLE auth.users (id UUID PRIMARY KEY)`); err != nil {
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

func forgetIdentity(t *testing.T, pool *pgxpool.Pool, owner shared.UserId) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`DELETE FROM auth.users WHERE id = $1`, owner.UUID()); err != nil {
		t.Fatalf("forget identity: %v", err)
	}
}

type storedRowsOf struct {
	history   int
	favorites int
	events    int
}

func countStoredRows(t *testing.T, pool *pgxpool.Pool, owner shared.UserId) storedRowsOf {
	t.Helper()
	return storedRowsOf{
		history:   countOwnedRows(t, pool, `SELECT COUNT(*) FROM discovery_search_history WHERE user_id = $1`, owner),
		favorites: countOwnedRows(t, pool, `SELECT COUNT(*) FROM discovery_favorites WHERE user_id = $1`, owner),
		events:    countOwnedRows(t, pool, `SELECT COUNT(*) FROM discovery_events WHERE user_id = $1`, owner),
	}
}

func countOwnedRows(t *testing.T, pool *pgxpool.Pool, countSQL string, owner shared.UserId) int {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), countSQL, owner.UUID()).Scan(&n); err != nil {
		t.Fatalf("count rows of %s: %v", owner.UUID(), err)
	}
	return n
}

func seedEveryDiscoveryTableFor(t *testing.T, pool *pgxpool.Pool, owner shared.UserId) {
	t.Helper()
	ctx := context.Background()
	t.Cleanup(func() { deleteStoredRowsOf(t, pool, owner) })

	history := NewPgxSearchHistoryRepository(pool)
	if err := history.Insert(ctx, &domain.SearchHistoryEntry{
		ID:         uuid.New(),
		UserId:     owner,
		Query:      "Mac DeMarco",
		QueryNorm:  "mac demarco",
		ExecutedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("seed search history: %v", err)
	}

	favorites := NewPgxFavoritesRepository(pool)
	if err := favorites.Add(ctx, owner, domain.Favorite{
		Kind:  domain.ResultKindArtist,
		Key:   "mac demarco",
		Title: "Mac DeMarco",
	}); err != nil {
		t.Fatalf("seed favorite: %v", err)
	}

	events := NewPgxEventStore(pool)
	if err := events.Append(ctx, domain.InteractionEvent{
		OccurredAt: time.Now().UTC(),
		UserId:     owner,
		Type:       domain.EventTypeSearchPerformed,
		QueryNorm:  "mac demarco",
		Payload:    map[string]any{},
	}); err != nil {
		t.Fatalf("seed event: %v", err)
	}
}

func deleteStoredRowsOf(t *testing.T, pool *pgxpool.Pool, owner shared.UserId) {
	t.Helper()
	for _, deleteSQL := range []string{
		`DELETE FROM discovery_search_history WHERE user_id = $1`,
		`DELETE FROM discovery_favorites WHERE user_id = $1`,
		`DELETE FROM discovery_events WHERE user_id = $1`,
	} {
		if _, err := pool.Exec(context.Background(), deleteSQL, owner.UUID()); err != nil {
			t.Fatalf("clean up rows of %s: %v", owner.UUID(), err)
		}
	}
}

func deletedIdentityErasers(pool *pgxpool.Pool) []ports.DeletedIdentityEraser {
	return []ports.DeletedIdentityEraser{
		NewPgxSearchHistoryRepository(pool),
		NewPgxFavoritesRepository(pool),
		NewPgxEventStore(pool),
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
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	standUpIdentityStore(t, pool)
	deleted := shared.NewUserId(uuid.New())
	stillRegistered := shared.NewUserId(uuid.New())
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
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	standUpIdentityStore(t, pool)
	system := shared.SystemUserId()
	deleted := shared.NewUserId(uuid.New())
	seedEveryDiscoveryTableFor(t, pool, system)
	seedEveryDiscoveryTableFor(t, pool, shared.NewUserId(uuid.New()))
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
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	standUpIdentityStore(t, pool)
	owner := shared.NewUserId(uuid.New())
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
	sharedtest.RequireIntegration(t)
	pool := testPool(t)
	var existing *string
	if err := pool.QueryRow(context.Background(),
		`SELECT to_regclass('auth.users')::text`).Scan(&existing); err != nil {
		t.Fatalf("look up the identity store: %v", err)
	}
	if existing != nil {
		t.Skip("auth.users exists here; the unreadable case cannot be reached")
	}
	owner := shared.NewUserId(uuid.New())
	seedEveryDiscoveryTableFor(t, pool, owner)

	for _, eraser := range deletedIdentityErasers(pool) {
		erased, err := eraser.EraseRowsOfDeletedIdentities(context.Background())
		if !errors.Is(err, ports.ErrIdentityStoreUnavailable) {
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
