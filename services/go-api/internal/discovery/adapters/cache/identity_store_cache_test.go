package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"fmt"
	"testing"
)

type fakeInnerIdentityStore struct {
	invalidated   []string
	invalidateErr error
}

func (f *fakeInnerIdentityStore) PersistBridges(context.Context, domain.ResultKind, string, map[string]string) error {
	return nil
}

func (f *fakeInnerIdentityStore) LookupByProviderID(context.Context, domain.ResultKind, domain.ProviderKey, string) (string, map[string]string, bool) {
	return "", nil, false
}

func (f *fakeInnerIdentityStore) Invalidate(_ context.Context, kind domain.ResultKind, provider domain.ProviderKey, externalID string) error {
	f.invalidated = append(f.invalidated, kind.String()+"|"+provider.String()+"|"+externalID)
	return f.invalidateErr
}

func TestRedisIdentityStore_Invalidate_DelegatesToDurableStore(t *testing.T) {
	inner := &fakeInnerIdentityStore{}
	store := NewRedisIdentityStore(inner, nil)

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

func TestRedisIdentityStore_PersistBridges_DropsCachedEntries(t *testing.T) {
	client := testRedisClient(t)
	mbid := fmt.Sprintf("qa-idmbid-%s", t.Name())
	extDeezer := fmt.Sprintf("dz-%s", t.Name())
	extSpotify := fmt.Sprintf("sp-%s", t.Name())
	xref := map[string]string{"deezer": extDeezer, "spotify": extSpotify}
	inner := &recordingIdentityStore{mbid: mbid, xref: xref, found: true}
	store := NewRedisIdentityStore(inner, client)
	ctx := context.Background()
	keys := map[string]string{
		"deezer":  identityKey(domain.ResultKindArtist, "deezer", extDeezer),
		"spotify": identityKey(domain.ResultKindArtist, "spotify", extSpotify),
	}
	cleanKeys(t, client, keys["deezer"], keys["spotify"])

	for provider, extID := range xref {
		if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, domain.ProviderKey(provider), extID); !ok {
			t.Fatalf("precondition: lookup (%s,%s) must hit", provider, extID)
		}
	}
	if inner.lookupCalls != 2 {
		t.Fatalf("precondition: durable lookups = %d, want 2 (backfill)", inner.lookupCalls)
	}

	if err := store.PersistBridges(ctx, domain.ResultKindArtist, mbid, xref); err != nil {
		t.Fatalf("PersistBridges: %v", err)
	}
	if inner.persistCalls != 1 {
		t.Fatalf("durable persist calls = %d, want 1 (durable first)", inner.persistCalls)
	}
	for provider, key := range keys {
		if n, err := client.Exists(ctx, key).Result(); err != nil || n != 0 {
			t.Errorf("Redis entry for %s survived persist (exists=%d, err=%v), want deleted", provider, n, err)
		}
	}

	for provider, extID := range xref {
		gotMBID, gotXref, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, domain.ProviderKey(provider), extID)
		if !ok || gotMBID != mbid {
			t.Errorf("lookup (%s,%s) = (%q,%v), want durable hit", provider, extID, gotMBID, ok)
		}
		if gotXref["deezer"] != extDeezer || gotXref["spotify"] != extSpotify {
			t.Errorf("lookup (%s,%s) xref = %v, want full bridge", provider, extID, gotXref)
		}
	}
	if inner.lookupCalls != 4 {
		t.Errorf("durable lookups = %d, want 4 (post-persist lookups refill from durable)", inner.lookupCalls)
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
	inner := &recordingIdentityStore{
		mbid: "qa-mbid-inval", xref: map[string]string{"deezer": extID}, found: true,
	}
	store := NewRedisIdentityStore(inner, client)
	ctx := context.Background()
	key := identityKey(domain.ResultKindArtist, "deezer", extID)
	cleanKeys(t, client, key)

	if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "deezer", extID); !ok {
		t.Fatal("precondition: lookup must hit")
	}
	if n, err := client.Exists(ctx, key).Result(); err != nil || n != 1 {
		t.Fatalf("precondition: Redis entry must be backfilled (exists=%d, err=%v)", n, err)
	}

	inner.found = false
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

func TestRedisIdentityStore_NilClient_DelegatesToInner(t *testing.T) {
	inner := &recordingIdentityStore{
		mbid: "mbid-1", xref: map[string]string{"deezer": "9"}, found: true,
	}
	store := NewRedisIdentityStore(inner, nil)
	ctx := context.Background()

	if err := store.PersistBridges(ctx, domain.ResultKindArtist, "mbid-1", map[string]string{"deezer": "9"}); err != nil {
		t.Fatalf("PersistBridges: %v", err)
	}
	if inner.persistCalls != 1 {
		t.Errorf("durable PersistBridges calls = %d, want 1", inner.persistCalls)
	}

	mbid, xref, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "deezer", "9")
	if !ok || mbid != "mbid-1" || xref["deezer"] != "9" {
		t.Errorf("nil-client lookup = (%q,%v,%v), want durable-store value", mbid, xref, ok)
	}
	if inner.lookupCalls != 1 {
		t.Errorf("durable lookup calls = %d, want 1", inner.lookupCalls)
	}
}

func TestIdentityKey_ByteIdenticalAcrossProviderKey(t *testing.T) {
	tests := []struct {
		kind       domain.ResultKind
		provider   domain.ProviderKey
		externalID string
		want       string
	}{
		{domain.ResultKindArtist, domain.ProviderKeyDeezer, "27", "discovery:identity:v1:artist:7fb1f072d9817471443055ebf5d7acd9"},
		{domain.ResultKindAlbum, domain.ProviderKeyITunes, "1440818839", "discovery:identity:v1:album:5c60b11e04600784205a0994464645d1"},
	}
	for _, tt := range tests {
		if got := identityKey(tt.kind, tt.provider, tt.externalID); got != tt.want {
			t.Errorf("identityKey(%v, %q, %q) = %q, want %q", tt.kind, tt.provider, tt.externalID, got, tt.want)
		}
	}
}

type mergingIdentityStore struct {
	mbid string
	xref map[string]string
}

func (m *mergingIdentityStore) PersistBridges(_ context.Context, _ domain.ResultKind, mbid string, xref map[string]string) error {
	m.mbid = mbid
	if m.xref == nil {
		m.xref = map[string]string{}
	}
	for provider, id := range xref {
		m.xref[provider] = id
	}
	return nil
}

func (m *mergingIdentityStore) LookupByProviderID(context.Context, domain.ResultKind, domain.ProviderKey, string) (string, map[string]string, bool) {
	return m.mbid, m.xref, m.mbid != ""
}

func (m *mergingIdentityStore) Invalidate(context.Context, domain.ResultKind, domain.ProviderKey, string) error {
	return nil
}

func TestRedisIdentityStore_PersistBridges_NarrowerXrefKeepsMergedUnion(t *testing.T) {
	client := testRedisClient(t)
	store := NewRedisIdentityStore(&mergingIdentityStore{}, client)
	ctx := context.Background()

	mbid := fmt.Sprintf("qa-idmbid-%s", t.Name())
	extDeezer := fmt.Sprintf("dz-%s", t.Name())
	extSpotify := fmt.Sprintf("sp-%s", t.Name())
	extITunes := fmt.Sprintf("it-%s", t.Name())
	full := map[string]string{"deezer": extDeezer, "spotify": extSpotify, "itunes": extITunes}
	cleanKeys(t, client,
		identityKey(domain.ResultKindArtist, "deezer", extDeezer),
		identityKey(domain.ResultKindArtist, "spotify", extSpotify),
		identityKey(domain.ResultKindArtist, "itunes", extITunes),
	)

	if err := store.PersistBridges(ctx, domain.ResultKindArtist, mbid, full); err != nil {
		t.Fatalf("PersistBridges full: %v", err)
	}
	if err := store.PersistBridges(ctx, domain.ResultKindArtist, mbid, map[string]string{"deezer": extDeezer}); err != nil {
		t.Fatalf("PersistBridges subset: %v", err)
	}

	gotMBID, gotXref, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "deezer", extDeezer)
	if !ok || gotMBID != mbid {
		t.Fatalf("lookup = (%q,%v), want hit", gotMBID, ok)
	}
	if len(gotXref) != 3 || gotXref["spotify"] != extSpotify || gotXref["itunes"] != extITunes {
		t.Errorf("xref = %v, want merged union of 3 providers", gotXref)
	}
}

func TestRedisIdentityStore_PersistBridges_RedisDelFailureStillReturnsNil(t *testing.T) {
	inner := &recordingIdentityStore{}
	store := NewRedisIdentityStore(inner, unreachableRedisClient(t))

	err := store.PersistBridges(context.Background(), domain.ResultKindArtist, "mbid-1", map[string]string{"deezer": "9"})
	if err != nil {
		t.Fatalf("PersistBridges = %v, want nil when Redis DEL fails", err)
	}
	if inner.persistCalls != 1 {
		t.Errorf("durable PersistBridges calls = %d, want 1", inner.persistCalls)
	}
}
