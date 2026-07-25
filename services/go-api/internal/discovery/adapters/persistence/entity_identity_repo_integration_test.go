//go:build integration

package persistence

import (
	"context"
	"os"
	"testing"

	"altune/go-api/internal/discovery/domain"

	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPgxIdentityStore_RoundTrip(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer pool.Close()

	store := NewPgxIdentityStore(pool)
	mbid := "0a68f3b5-79c2-4f81-a7bc-ebc977602e86"
	xref := map[string]string{"deezer": "234701081", "discogs": "987654"}

	_, _ = pool.Exec(ctx, `DELETE FROM entity_identity WHERE external_id IN ('234701081','987654')`)

	if err := store.PersistBridges(ctx, domain.ResultKindArtist, mbid, xref); err != nil {
		t.Fatalf("PersistBridges: %v", err)
	}

	for provider, externalID := range xref {
		gotMBID, gotXref, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, provider, externalID)
		if !ok {
			t.Errorf("lookup (%s,%s): not found, want hit", provider, externalID)
			continue
		}
		if gotMBID != mbid {
			t.Errorf("lookup (%s,%s): mbid = %q, want %q", provider, externalID, gotMBID, mbid)
		}
		if gotXref["deezer"] != "234701081" || gotXref["discogs"] != "987654" {
			t.Errorf("lookup (%s,%s): xref = %v, want full bridge", provider, externalID, gotXref)
		}
	}

	if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindAlbum, "deezer", "234701081"); ok {
		t.Error("lookup as album hit, want miss (kind is part of the key)")
	}

	if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "deezer", "does-not-exist"); ok {
		t.Error("lookup of unknown id hit, want miss")
	}

	newMBID := "11111111-2222-3333-4444-555555555555"
	if err := store.PersistBridges(ctx, domain.ResultKindArtist, newMBID, map[string]string{"deezer": "234701081"}); err != nil {
		t.Fatalf("re-PersistBridges: %v", err)
	}
	gotMBID, gotXref, _ := store.LookupByProviderID(ctx, domain.ResultKindArtist, "deezer", "234701081")
	if gotMBID != newMBID {
		t.Errorf("after upsert mbid = %q, want %q", gotMBID, newMBID)
	}
	if gotXref["discogs"] != "987654" {
		t.Errorf("partial re-learn erased the discogs edge, xref = %v", gotXref)
	}
	if gotXref["deezer"] != "234701081" {
		t.Errorf("xref lost the re-learned deezer edge, xref = %v", gotXref)
	}

	if err := store.Invalidate(ctx, domain.ResultKindArtist, "deezer", "234701081"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "deezer", "234701081"); ok {
		t.Error("invalidated identity still resolves, want miss")
	}
	if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "discogs", "987654"); !ok {
		t.Error("sibling row was deleted by Invalidate, want it untouched")
	}
	if err := store.Invalidate(ctx, domain.ResultKindArtist, "deezer", "does-not-exist"); err != nil {
		t.Errorf("Invalidate of missing row: %v, want nil", err)
	}

	_, _ = pool.Exec(ctx, `DELETE FROM entity_identity WHERE external_id IN ('234701081','987654')`)
}
