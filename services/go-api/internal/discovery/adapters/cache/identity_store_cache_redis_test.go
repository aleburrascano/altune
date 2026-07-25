package cache

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestRedisIdentityStore_PersistBridges_WarmsCache(t *testing.T) {
	client := testRedisClient(t)
	inner := &recordingIdentityStore{}
	store := NewRedisIdentityStore(inner, client)
	ctx := context.Background()

	mbid := fmt.Sprintf("qa-idmbid-%s", t.Name())
	extDeezer := fmt.Sprintf("dz-%s", t.Name())
	extSpotify := fmt.Sprintf("sp-%s", t.Name())
	xref := map[string]string{"deezer": extDeezer, "spotify": extSpotify}
	cleanKeys(t, client,
		identityKey(domain.ResultKindArtist, "deezer", extDeezer),
		identityKey(domain.ResultKindArtist, "spotify", extSpotify),
	)

	if err := store.PersistBridges(ctx, domain.ResultKindArtist, mbid, xref); err != nil {
		t.Fatalf("PersistBridges: %v", err)
	}
	if inner.persistCalls != 1 {
		t.Fatalf("durable persist calls = %d, want 1 (durable first)", inner.persistCalls)
	}

	for provider, extID := range xref {
		gotMBID, gotXref, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, provider, extID)
		if !ok || gotMBID != mbid {
			t.Errorf("lookup (%s,%s) = (%q,%v), want warmed hit", provider, extID, gotMBID, ok)
		}
		if gotXref["deezer"] != extDeezer || gotXref["spotify"] != extSpotify {
			t.Errorf("lookup (%s,%s) xref = %v, want full bridge", provider, extID, gotXref)
		}
	}
	if inner.lookupCalls != 0 {
		t.Errorf("durable lookups = %d, want 0 (warmed cache must answer)", inner.lookupCalls)
	}

	if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindAlbum, "deezer", extDeezer); ok {
		t.Error("artist cache entry answered an album lookup, want kind-isolated miss")
	}
	if inner.lookupCalls != 1 {
		t.Errorf("durable lookups after cross-kind lookup = %d, want 1", inner.lookupCalls)
	}
}

func TestRedisIdentityStore_Lookup_ReadThroughBackfill(t *testing.T) {
	client := testRedisClient(t)
	extID := fmt.Sprintf("dz-backfill-%s", t.Name())
	inner := &recordingIdentityStore{
		mbid: "qa-mbid-backfill", xref: map[string]string{"deezer": extID}, found: true,
	}
	store := NewRedisIdentityStore(inner, client)
	ctx := context.Background()
	cleanKeys(t, client, identityKey(domain.ResultKindAlbum, "deezer", extID))

	for i := 1; i <= 2; i++ {
		mbid, xref, ok := store.LookupByProviderID(ctx, domain.ResultKindAlbum, "deezer", extID)
		if !ok || mbid != "qa-mbid-backfill" || xref["deezer"] != extID {
			t.Fatalf("lookup #%d = (%q,%v,%v), want durable value", i, mbid, xref, ok)
		}
	}
	if inner.lookupCalls != 1 {
		t.Errorf("durable lookups = %d, want 1 (second lookup served by back-filled cache)", inner.lookupCalls)
	}

	missInner := &recordingIdentityStore{}
	missStore := NewRedisIdentityStore(missInner, client)
	extMiss := fmt.Sprintf("dz-miss-%s", t.Name())
	cleanKeys(t, client, identityKey(domain.ResultKindAlbum, "deezer", extMiss))
	for i := 1; i <= 2; i++ {
		if _, _, ok := missStore.LookupByProviderID(ctx, domain.ResultKindAlbum, "deezer", extMiss); ok {
			t.Fatalf("lookup #%d of unknown id hit, want miss", i)
		}
	}
	if missInner.lookupCalls != 2 {
		t.Errorf("durable lookups on miss = %d, want 2 (misses are not negative-cached)", missInner.lookupCalls)
	}
}

func TestRedisIdentityStore_Invalidate_PurgesRedisEvenOnDurableError(t *testing.T) {
	client := testRedisClient(t)
	extID := fmt.Sprintf("dz-inval-%s", t.Name())
	inner := &recordingIdentityStore{}
	store := NewRedisIdentityStore(inner, client)
	ctx := context.Background()
	key := identityKey(domain.ResultKindArtist, "deezer", extID)
	cleanKeys(t, client, key)

	if err := store.PersistBridges(ctx, domain.ResultKindArtist, "qa-mbid-inval",
		map[string]string{"deezer": extID}); err != nil {
		t.Fatalf("PersistBridges: %v", err)
	}
	if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "deezer", extID); !ok {
		t.Fatal("precondition: warmed lookup must hit")
	}

	inner.invalidateErr = errors.New("pg down")
	if err := store.Invalidate(ctx, domain.ResultKindArtist, "deezer", extID); err == nil {
		t.Error("Invalidate swallowed the durable-store error")
	}
	if n, err := client.Exists(ctx, key).Result(); err != nil || n != 0 {
		t.Errorf("Redis entry survived a failed durable purge (exists=%d, err=%v), want deleted", n, err)
	}
	if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "deezer", extID); ok {
		t.Error("invalidated identity still served from cache")
	}
}
