package persistence_test

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/shared/testdb"
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPostgresIdentityStore_RoundTrip(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t)

	store := persistence.NewPostgresIdentityStore(pool)
	mbid := "0a68f3b5-79c2-4f81-a7bc-ebc977602e86"
	xref := map[domain.ProviderKey]string{"deezer": "234701081", "discogs": "987654"}

	_, _ = pool.Exec(ctx, `DELETE FROM entity_identity WHERE external_id IN ('234701081','987654')`)

	seedIdentity(t, pool, domain.ResultKindArtist, mbid, xref)

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

	_, _ = pool.Exec(ctx, `DELETE FROM entity_identity WHERE external_id IN ('234701081','987654')`)
}

type queryCounter struct{ n atomic.Int64 }

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func queryCountingPool(t *testing.T) (*pgxpool.Pool, *queryCounter) {
	t.Helper()
	counter := &queryCounter{}
	return testdb.TracedPool(t, counter), counter
}

func TestPostgresIdentityStore_LookupByProviderIDsIsOneQuery(t *testing.T) {
	testdb.RequireIntegration(t)
	pool, counter := queryCountingPool(t)
	ctx := context.Background()
	store := persistence.NewPostgresIdentityStore(pool)
	cleanup := func() {
		_, _ = pool.Exec(ctx, `DELETE FROM entity_identity WHERE external_id LIKE $1`, "%"+batchTestIDPrefix+"%")
	}
	cleanup()
	t.Cleanup(cleanup)

	refs := make([]ports.IdentityRef, 50)
	for i := range refs {
		id := batchTestIDPrefix + strconv.Itoa(i)
		refs[i] = ports.IdentityRef{Kind: domain.ResultKindTrack, Provider: domain.ProviderKeyDeezer, ExternalID: id}
		if i%2 == 0 {
			seedIdentity(t, pool, domain.ResultKindTrack, "mbid-"+id, map[domain.ProviderKey]string{"deezer": id, "discogs": "d" + id})
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

func TestPostgresIdentityStore_LookupByProviderIDsKeysOnKind(t *testing.T) {
	testdb.RequireIntegration(t)
	pool, _ := queryCountingPool(t)
	ctx := context.Background()
	store := persistence.NewPostgresIdentityStore(pool)
	id := batchTestIDPrefix + "kind"
	cleanup := func() { _, _ = pool.Exec(ctx, `DELETE FROM entity_identity WHERE external_id = $1`, id) }
	cleanup()
	t.Cleanup(cleanup)

	seedIdentity(t, pool, domain.ResultKindArtist, "artist-mbid", map[domain.ProviderKey]string{"deezer": id})
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

func TestPostgresIdentityStore_LookupByProviderID_CorruptXrefWarnsAndKeepsHit(t *testing.T) {
	ctx := context.Background()
	pool := testdb.Pool(t)
	_, _ = pool.Exec(ctx, `DELETE FROM entity_identity WHERE external_id = 'corrupt-xref-595'`)
	if _, err := pool.Exec(ctx,
		`INSERT INTO entity_identity (provider, external_id, kind, mbid, xref) VALUES ('deezer', 'corrupt-xref-595', 'artist', 'mbid-c', '[1]')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	defer pool.Exec(ctx, `DELETE FROM entity_identity WHERE external_id = 'corrupt-xref-595'`)
	logs := captureWarnings(t)

	mbid, xref, ok := persistence.NewPostgresIdentityStore(pool).LookupByProviderID(ctx, domain.ResultKindArtist, "deezer", "corrupt-xref-595")

	if !ok || mbid != "mbid-c" || len(xref) != 0 {
		t.Errorf("got (%q, %v, %v), want hit mbid-c with empty xref", mbid, xref, ok)
	}
	if !strings.Contains(logs.String(), "identity.xref_corrupt") {
		t.Errorf("corrupt xref not logged at Warn: %q", logs.String())
	}
}

func TestPostgresIdentityStore_LookupByProviderIDs_CorruptXrefWarnsAndKeepsHit(t *testing.T) {
	testdb.RequireIntegration(t)
	ctx := context.Background()
	pool := testdb.Pool(t)
	_, _ = pool.Exec(ctx, `DELETE FROM entity_identity WHERE external_id = 'corrupt-xref-batch'`)
	if _, err := pool.Exec(ctx,
		`INSERT INTO entity_identity (provider, external_id, kind, mbid, xref) VALUES ('deezer', 'corrupt-xref-batch', 'artist', 'mbid-1', '["not","a","map"]')`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM entity_identity WHERE external_id = 'corrupt-xref-batch'`)
	})
	logs := captureWarnings(t)
	ref := ports.IdentityRef{Kind: domain.ResultKindArtist, Provider: "deezer", ExternalID: "corrupt-xref-batch"}

	hits := persistence.NewPostgresIdentityStore(pool).LookupByProviderIDs(ctx, []ports.IdentityRef{ref})

	if hit, ok := hits[ref]; !ok || hit.MBID != "mbid-1" || len(hit.Xref) != 0 {
		t.Errorf("hit = %+v ok=%v, want mbid-1 with empty xref", hit, ok)
	}
	if !strings.Contains(logs.String(), "identity.xref_corrupt") {
		t.Errorf("corrupt xref not logged at Warn: %q", logs.String())
	}
}

func seedIdentity(t *testing.T, pool *pgxpool.Pool, kind domain.ResultKind, mbid string, xref map[domain.ProviderKey]string) {
	t.Helper()
	blob, err := json.Marshal(xref)
	if err != nil {
		t.Fatalf("marshal xref: %v", err)
	}
	for provider, externalID := range xref {
		if _, err := pool.Exec(context.Background(),
			`INSERT INTO entity_identity (provider, external_id, kind, mbid, xref) VALUES ($1, $2, $3, $4, $5)`,
			provider, externalID, kind.String(), mbid, blob); err != nil {
			t.Fatalf("seed %s:%s: %v", provider, externalID, err)
		}
	}
}
