package persistence

import (
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func identityStoreTx(t *testing.T) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := testPool(t).Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })

	var existing *string
	if err := tx.QueryRow(ctx, `SELECT to_regclass('auth.users')::text`).Scan(&existing); err != nil {
		t.Fatalf("look up the identity store: %v", err)
	}
	if existing != nil {
		t.Skip("auth.users is the real identity store here; this test will not write to it")
	}
	if _, err := tx.Exec(ctx, `CREATE SCHEMA auth; CREATE TABLE auth.users (id UUID PRIMARY KEY)`); err != nil {
		t.Fatalf("create the identity store: %v", err)
	}
	return tx
}

func storeQueueOf(t *testing.T, tx pgx.Tx, owner shared.UserId) {
	t.Helper()
	_, err := tx.Exec(context.Background(),
		`INSERT INTO playback_queue_state (user_id, track_ids, source_id) VALUES ($1, $2, $3)`,
		owner.UUID(), []string{"a", "b"}, "search:mac demarco")
	if err != nil {
		t.Fatalf("store queue state: %v", err)
	}
}

func registerIdentity(t *testing.T, tx pgx.Tx, owner shared.UserId) {
	t.Helper()
	if _, err := tx.Exec(context.Background(), `INSERT INTO auth.users (id) VALUES ($1)`, owner.UUID()); err != nil {
		t.Fatalf("register identity: %v", err)
	}
}

func orphanedOwners(t *testing.T, repo *PgxDeletedIdentityRepository) []shared.UserId {
	t.Helper()
	var owners []shared.UserId
	var after shared.UserId
	for {
		page, err := repo.ListOwnersWithoutIdentity(context.Background(), after, 1000)
		if err != nil {
			t.Fatalf("ListOwnersWithoutIdentity: %v", err)
		}
		owners = append(owners, page.Orphaned...)
		if page.Done {
			return owners
		}
		after = page.Next
	}
}

func contains(owners []shared.UserId, owner shared.UserId) bool {
	for _, o := range owners {
		if o == owner {
			return true
		}
	}
	return false
}

func TestListOwnersWithoutIdentity_FindsOnlyOwnersWhoseAccountIsGone(t *testing.T) {
	sharedtest.RequireIntegration(t)
	tx := identityStoreTx(t)
	repo := &PgxDeletedIdentityRepository{pool: tx}
	deleted := shared.NewUserId(uuid.New())
	stillRegistered := shared.NewUserId(uuid.New())
	storeQueueOf(t, tx, deleted)
	storeQueueOf(t, tx, stillRegistered)
	registerIdentity(t, tx, stillRegistered)

	owners := orphanedOwners(t, repo)

	if !contains(owners, deleted) {
		t.Errorf("the owner whose account was deleted is not up for erasure; got %v", owners)
	}
	if contains(owners, stillRegistered) {
		t.Error("an owner whose account still exists was offered up for erasure")
	}
}

func TestListOwnersWithoutIdentity_EmptyIdentityStoreOffersNoOne(t *testing.T) {
	sharedtest.RequireIntegration(t)
	tx := identityStoreTx(t)
	repo := &PgxDeletedIdentityRepository{pool: tx}
	storeQueueOf(t, tx, shared.NewUserId(uuid.New()))

	owners := orphanedOwners(t, repo)

	if len(owners) != 0 {
		t.Errorf("an identity store showing no identities offered %d accounts for erasure, want 0", len(owners))
	}
}

func TestListOwnersWithoutIdentity_PagesThroughOwnersInKeyOrder(t *testing.T) {
	sharedtest.RequireIntegration(t)
	tx := identityStoreTx(t)
	repo := &PgxDeletedIdentityRepository{pool: tx}
	if _, err := tx.Exec(context.Background(), `DELETE FROM playback_queue_state`); err != nil {
		t.Fatalf("clear queue states: %v", err)
	}
	owners := []shared.UserId{
		shared.NewUserId(uuid.MustParse("10000000-0000-4000-8000-000000000000")),
		shared.NewUserId(uuid.MustParse("20000000-0000-4000-8000-000000000000")),
		shared.NewUserId(uuid.MustParse("30000000-0000-4000-8000-000000000000")),
	}
	for _, owner := range owners {
		storeQueueOf(t, tx, owner)
	}
	registerIdentity(t, tx, owners[0])

	first, err := repo.ListOwnersWithoutIdentity(context.Background(), shared.UserId{}, 2)
	if err != nil {
		t.Fatalf("ListOwnersWithoutIdentity(first page): %v", err)
	}
	if first.Done || first.Next != owners[1] || len(first.Orphaned) != 1 || first.Orphaned[0] != owners[1] {
		t.Fatalf("first page = %+v, want owner 2 orphaned, the cursor on owner 2 and more to scan", first)
	}

	second, err := repo.ListOwnersWithoutIdentity(context.Background(), first.Next, 2)
	if err != nil {
		t.Fatalf("ListOwnersWithoutIdentity(second page): %v", err)
	}
	if !second.Done || second.Next != owners[2] || len(second.Orphaned) != 1 || second.Orphaned[0] != owners[2] {
		t.Fatalf("second page = %+v, want owner 3 orphaned and the pass done", second)
	}
}
