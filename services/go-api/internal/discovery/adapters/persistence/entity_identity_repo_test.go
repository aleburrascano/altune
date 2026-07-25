package persistence

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestPgxIdentityStore_EmptyInputGuards(t *testing.T) {
	store := NewPgxIdentityStore(nil)
	ctx := context.Background()

	t.Run("PersistBridges no-ops on empty mbid or empty xref", func(t *testing.T) {
		if err := store.PersistBridges(ctx, domain.ResultKindArtist, "", map[string]string{"deezer": "1"}); err != nil {
			t.Errorf("empty mbid: %v, want nil no-op", err)
		}
		if err := store.PersistBridges(ctx, domain.ResultKindArtist, "some-mbid", nil); err != nil {
			t.Errorf("nil xref: %v, want nil no-op", err)
		}
	})

	t.Run("PersistBridges skips blank providers and ids", func(t *testing.T) {
		xref := map[string]string{"": "123", "deezer": ""}
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

	t.Run("Invalidate no-ops on empty inputs", func(t *testing.T) {
		if err := store.Invalidate(ctx, domain.ResultKindArtist, "", "123"); err != nil {
			t.Errorf("empty provider: %v, want nil no-op", err)
		}
		if err := store.Invalidate(ctx, domain.ResultKindArtist, "deezer", ""); err != nil {
			t.Errorf("empty external id: %v, want nil no-op", err)
		}
	})
}
