package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"testing"
)

func TestRedisIdentityStore_Lookup_ReadThroughBackfill(t *testing.T) {
	client := testRedisClient(t)
	extID := fmt.Sprintf("dz-backfill-%s", t.Name())
	inner := &recordingIdentityStore{
		mbid: "qa-mbid-backfill", xref: map[domain.ProviderKey]string{"deezer": extID}, found: true,
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

func TestRedisIdentityStore_NilClient_DelegatesToInner(t *testing.T) {
	inner := &recordingIdentityStore{
		mbid: "mbid-1", xref: map[domain.ProviderKey]string{"deezer": "9"}, found: true,
	}
	store := NewRedisIdentityStore(inner, nil)
	ctx := context.Background()

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
