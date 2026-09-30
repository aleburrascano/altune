package persistence

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestPgxIdentityStore_EmptyInputGuards(t *testing.T) {
	store := NewPgxIdentityStore(nil)
	ctx := context.Background()

	t.Run("PersistBridges no-ops on empty mbid or empty xref", func(t *testing.T) {
		if err := store.PersistBridges(ctx, domain.ResultKindArtist, "", map[domain.ProviderKey]string{"deezer": "1"}); err != nil {
			t.Errorf("empty mbid: %v, want nil no-op", err)
		}
		if err := store.PersistBridges(ctx, domain.ResultKindArtist, "some-mbid", nil); err != nil {
			t.Errorf("nil xref: %v, want nil no-op", err)
		}
	})

	t.Run("PersistBridges skips blank providers and ids", func(t *testing.T) {
		xref := map[domain.ProviderKey]string{"": "123", "deezer": ""}
		if err := store.PersistBridges(ctx, domain.ResultKindArtist, "some-mbid", xref); err != nil {
			t.Errorf("all-blank xref: %v, want nil no-op", err)
		}
	})

	t.Run("LookupByProviderID misses on empty inputs", func(t *testing.T) {
		if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "", "123"); ok {
			t.Error("empty provider: got hit, want miss")
		}
		if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "deezer", ""); ok {
			t.Error("empty external id: got hit, want miss")
		}
	})

	t.Run("LookupByProviderIDs skips the query when every ref is blank", func(t *testing.T) {
		refs := []ports.IdentityRef{{Kind: domain.ResultKindArtist, ExternalID: "123"}, {Kind: domain.ResultKindArtist, Provider: "deezer"}}
		if hits := store.LookupByProviderIDs(ctx, refs); len(hits) != 0 {
			t.Errorf("blank refs: hits = %v, want none", hits)
		}
		if hits := store.LookupByProviderIDs(ctx, nil); len(hits) != 0 {
			t.Errorf("nil refs: hits = %v, want none", hits)
		}
	})

	t.Run("Invalidate no-ops on empty inputs", func(t *testing.T) {
		if err := store.Invalidate(ctx, domain.ResultKindArtist, "", "123"); err != nil {
			t.Errorf("empty provider: %v, want nil no-op", err)
		}
		if err := store.Invalidate(ctx, domain.ResultKindArtist, "deezer", ""); err != nil {
			t.Errorf("empty external id: %v, want nil no-op", err)
		}
	})
}

type stubIdentityRows struct {
	pgx.Rows
	data [][5]any
	pos  int
}

func (r *stubIdentityRows) Next() bool { r.pos++; return r.pos <= len(r.data) }
func (r *stubIdentityRows) Close()     {}
func (r *stubIdentityRows) Err() error { return nil }
func (r *stubIdentityRows) Scan(dest ...any) error {
	for i, d := range r.data[r.pos-1] {
		switch p := dest[i].(type) {
		case *string:
			*p = d.(string)
		case *[]byte:
			*p = d.([]byte)
		}
	}
	return nil
}

func captureWarnings(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return &buf
}

func TestPgxIdentityStore_LookupByProviderID_DBFailureWarnsAndMisses(t *testing.T) {
	logs := captureWarnings(t)
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/db?connect_timeout=1")
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	defer pool.Close()

	_, _, ok := NewPgxIdentityStore(pool).LookupByProviderID(context.Background(), domain.ResultKindArtist, "deezer", "1")

	if ok {
		t.Error("db failure: got hit, want miss")
	}
	if !strings.Contains(logs.String(), "identity.lookup_failed") {
		t.Errorf("db failure not logged at Warn: %q", logs.String())
	}
}

func TestPgxIdentityStore_LookupByProviderIDs_CorruptXrefWarnsAndKeepsHit(t *testing.T) {
	logs := captureWarnings(t)
	ref := ports.IdentityRef{Kind: domain.ResultKindArtist, Provider: "deezer", ExternalID: "1"}
	requested := identityRefsByRow([]ports.IdentityRef{ref})
	rows := &stubIdentityRows{data: [][5]any{{"deezer", "1", domain.ResultKindArtist.String(), "mbid-1", []byte(`["not","a","map"]`)}}}

	hits, err := scanIdentityHits(context.Background(), rows, requested)
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if hit, ok := hits[ref]; !ok || hit.MBID != "mbid-1" || len(hit.Xref) != 0 {
		t.Errorf("hit = %+v ok=%v, want mbid-1 with empty xref", hit, ok)
	}
	if !strings.Contains(logs.String(), "identity.xref_corrupt") {
		t.Errorf("corrupt xref not logged at Warn: %q", logs.String())
	}
}

func TestPgxIdentityStore_PersistBridges_QueuesRowsInSortedProviderOrder(t *testing.T) {
	xref := map[domain.ProviderKey]string{"spotify": "s1", "deezer": "d1", "apple": "a1", "tidal": "t1", "itunes": "i1", "lastfm": "l1"}

	for range 20 {
		batch := bridgeBatch(domain.ResultKindArtist, "mbid-1", xref, []byte("{}"))

		var got []string
		for _, q := range batch.QueuedQueries {
			got = append(got, fmt.Sprint(q.Arguments[0]))
		}
		want := []string{"apple", "deezer", "itunes", "lastfm", "spotify", "tidal"}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("queued provider order = %v, want %v", got, want)
		}
	}
}
