//go:build integration

package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"os"
	"strconv"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
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
		gotMBID, gotXref, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, domain.ProviderKey(provider), externalID)
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

// queryCounter is a pgx tracer that counts statements sent to Postgres, so the
// batch lookup's round-trip cost is asserted against a real database.
type queryCounter struct{ n atomic.Int64 }

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func queryCountingPool(t *testing.T) (*pgxpool.Pool, *queryCounter) {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("parse DATABASE_URL: %v", err)
	}
	counter := &queryCounter{}
	cfg.ConnConfig.Tracer = counter
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool, counter
}

func TestPgxIdentityStore_LookupByProviderIDsIsOneQuery(t *testing.T) {
	pool, counter := queryCountingPool(t)
	ctx := context.Background()
	store := NewPgxIdentityStore(pool)
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM entity_identity WHERE external_id LIKE $1`, batchTestIDPrefix+"%")
	}
	cleanup()
	t.Cleanup(cleanup)

	refs := make([]ports.IdentityRef, 50)
	for i := range refs {
		id := batchTestIDPrefix + strconv.Itoa(i)
		refs[i] = ports.IdentityRef{Kind: domain.ResultKindTrack, Provider: domain.ProviderKeyDeezer, ExternalID: id}
		if i%2 == 0 {
			if err := store.PersistBridges(ctx, domain.ResultKindTrack, "mbid-"+id, map[string]string{"deezer": id, "discogs": "d" + id}); err != nil {
				t.Fatalf("seed %s: %v", id, err)
			}
		}
	}

	counter.n.Store(0)
	for _, ref := range refs {
		store.LookupByProviderID(ctx, ref.Kind, ref.Provider, ref.ExternalID)
	}
	if got := counter.n.Load(); got != 50 {
		t.Errorf("per-ref lookups issued %d queries, want 50", got)
	}

	counter.n.Store(0)
	hits := store.LookupByProviderIDs(ctx, refs)
	if got := counter.n.Load(); got != 1 {
		t.Errorf("batch lookup issued %d queries, want 1", got)
	}
	t.Logf("50 refs: per-ref = 50 queries, batch = %d query", counter.n.Load())

	if len(hits) != 25 {
		t.Errorf("hits = %d, want 25 (only even refs were seeded)", len(hits))
	}
	for i, ref := range refs {
		want := "d" + ref.ExternalID
		hit, ok := hits[ref]
		if i%2 != 0 {
			if ok {
				t.Errorf("ref %s: hit %v, want miss", ref.ExternalID, hit)
			}
			continue
		}
		if !ok || hit.MBID != "mbid-"+ref.ExternalID || hit.Xref["discogs"] != want {
			t.Errorf("ref %s: hit = %v ok=%v, want mbid and discogs=%q", ref.ExternalID, hit, ok, want)
		}
	}
}

func TestPgxIdentityStore_LookupByProviderIDsKeysOnKind(t *testing.T) {
	pool, _ := queryCountingPool(t)
	ctx := context.Background()
	store := NewPgxIdentityStore(pool)
	id := batchTestIDPrefix + "kind"
	cleanup := func() { _, _ = pool.Exec(ctx, `DELETE FROM entity_identity WHERE external_id = $1`, id) }
	cleanup()
	t.Cleanup(cleanup)

	if err := store.PersistBridges(ctx, domain.ResultKindArtist, "artist-mbid", map[string]string{"deezer": id}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	artist := ports.IdentityRef{Kind: domain.ResultKindArtist, Provider: domain.ProviderKeyDeezer, ExternalID: id}
	album := ports.IdentityRef{Kind: domain.ResultKindAlbum, Provider: domain.ProviderKeyDeezer, ExternalID: id}

	hits := store.LookupByProviderIDs(ctx, []ports.IdentityRef{artist, album, {Kind: domain.ResultKindArtist}})

	if hits[artist].MBID != "artist-mbid" {
		t.Errorf("artist ref: hit = %v, want artist-mbid", hits[artist])
	}
	if _, ok := hits[album]; ok || len(hits) != 1 {
		t.Errorf("hits = %v, want only the artist ref (kind is part of the key)", hits)
	}
}

const batchTestIDPrefix = "batch-1106-"
