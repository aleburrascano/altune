package cache

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

// fakeInnerIdentityStore records calls so the cache's delegation is observable.
type fakeInnerIdentityStore struct {
	invalidated   []string
	invalidateErr error
}

func (f *fakeInnerIdentityStore) PersistBridges(context.Context, domain.ResultKind, string, map[string]string) error {
	return nil
}

func (f *fakeInnerIdentityStore) LookupByProviderID(context.Context, domain.ResultKind, string, string) (string, map[string]string, bool) {
	return "", nil, false
}

func (f *fakeInnerIdentityStore) Invalidate(_ context.Context, kind domain.ResultKind, provider, externalID string) error {
	f.invalidated = append(f.invalidated, kind.String()+"|"+provider+"|"+externalID)
	return f.invalidateErr
}

func TestRedisIdentityStore_Invalidate_DelegatesToDurableStore(t *testing.T) {
	inner := &fakeInnerIdentityStore{}
	store := NewRedisIdentityStore(inner, nil) // nil client: durable-only degradation

	if err := store.Invalidate(context.Background(), domain.ResultKindArtist, "deezer", "123"); err != nil {
		t.Fatalf("Invalidate: %v", err)
	}
	if len(inner.invalidated) != 1 || inner.invalidated[0] != "artist|deezer|123" {
		t.Errorf("durable Invalidate not delegated, got %v", inner.invalidated)
	}
}

func TestRedisIdentityStore_Invalidate_SurfacesDurableError(t *testing.T) {
	inner := &fakeInnerIdentityStore{invalidateErr: errors.New("pg down")}
	store := NewRedisIdentityStore(inner, nil)

	if err := store.Invalidate(context.Background(), domain.ResultKindArtist, "deezer", "123"); err == nil {
		t.Fatal("expected the durable-store error to surface")
	}
}
