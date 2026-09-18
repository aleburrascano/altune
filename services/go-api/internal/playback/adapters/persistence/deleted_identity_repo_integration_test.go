//go:build integration

package persistence

import (
	"altune/go-api/internal/shared"
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// identityStoreTx stands an identity store up inside a transaction that is
// always rolled back, so the anti-join runs against a real Postgres without the
// test owning a schema Supabase owns in production. A database that already has
// auth.users is that production identity store: the test skips rather than
// writing to it.
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

func contains(owners []shared.UserId, owner shared.UserId) bool {
	for _, o := range owners {
		if o == owner {
			return true
		}
	}
	return false
}

// TestListOwnersWithoutIdentity_FindsOnlyOwnersWhoseAccountIsGone runs the
// anti-join against a real Postgres: the erasure sweep it feeds is irreversible,
// so "which accounts no longer exist" has to be answered by the database rather
// than by a fake that agrees with the Go code.
func TestListOwnersWithoutIdentity_FindsOnlyOwnersWhoseAccountIsGone(t *testing.T) {
	tx := identityStoreTx(t)
	repo := &PgxDeletedIdentityRepository{pool: tx}
	deleted := shared.NewUserId(uuid.New())
	stillRegistered := shared.NewUserId(uuid.New())
	storeQueueOf(t, tx, deleted)
	storeQueueOf(t, tx, stillRegistered)
	registerIdentity(t, tx, stillRegistered)

	owners, err := repo.ListOwnersWithoutIdentity(context.Background(), 1000)
	if err != nil {
		t.Fatalf("ListOwnersWithoutIdentity: %v", err)
	}

	if !contains(owners, deleted) {
		t.Errorf("the owner whose account was deleted is not up for erasure; got %v", owners)
	}
	if contains(owners, stillRegistered) {
		t.Error("an owner whose account still exists was offered up for erasure")
	}
}

// TestListOwnersWithoutIdentity_EmptyIdentityStoreOffersNoOne pins the blast
// bound in the SQL: an identity store this role reaches but sees no rows in
// (row-level security, a restore still in flight) would otherwise make every
// stored queue look like a deleted account's and erase the whole table.
func TestListOwnersWithoutIdentity_EmptyIdentityStoreOffersNoOne(t *testing.T) {
	tx := identityStoreTx(t)
	repo := &PgxDeletedIdentityRepository{pool: tx}
	storeQueueOf(t, tx, shared.NewUserId(uuid.New()))

	owners, err := repo.ListOwnersWithoutIdentity(context.Background(), 1000)
	if err != nil {
		t.Fatalf("ListOwnersWithoutIdentity: %v", err)
	}

	if len(owners) != 0 {
		t.Errorf("an identity store showing no identities offered %d accounts for erasure, want 0", len(owners))
	}
}
