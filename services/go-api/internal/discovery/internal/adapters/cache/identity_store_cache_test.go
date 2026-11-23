package cache_test

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/adapters/cache"
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

const deezerArtist27IdentityKey = "discovery:identity:v1:artist:7fb1f072d9817471443055ebf5d7acd9"

func TestRedisIdentityStore_Lookup_ReadThroughBackfill(t *testing.T) {
	_, client := newRedis(t)
	inner := &recordingIdentityStore{
		mbid: "qa-mbid-backfill", xref: map[domain.ProviderKey]string{"deezer": "dz-1"}, found: true,
	}
	store := cache.NewRedisIdentityStore(inner, client)
	ctx := context.Background()

	for i := 1; i <= 2; i++ {
		mbid, xref, ok := store.LookupByProviderID(ctx, domain.ResultKindAlbum, "deezer", "dz-1")
		if !ok || mbid != "qa-mbid-backfill" || xref["deezer"] != "dz-1" {
			t.Fatalf("lookup #%d = (%q,%v,%v), want durable value", i, mbid, xref, ok)
		}
	}
	if inner.calls() != 1 {
		t.Errorf("durable lookups = %d, want 1 (second lookup served by back-filled cache)", inner.calls())
	}

	missInner := &recordingIdentityStore{}
	missStore := cache.NewRedisIdentityStore(missInner, client)
	for i := 1; i <= 2; i++ {
		if _, _, ok := missStore.LookupByProviderID(ctx, domain.ResultKindAlbum, "deezer", "dz-miss"); ok {
			t.Fatalf("lookup #%d of unknown id hit, want miss", i)
		}
	}
	if missInner.calls() != 2 {
		t.Errorf("durable lookups on miss = %d, want 2 (misses are not negative-cached)", missInner.calls())
	}
}

func TestRedisIdentityStore_BackfillsUnderTheV1KeyForThirtyDays(t *testing.T) {
	server, client := newRedis(t)
	inner := &recordingIdentityStore{mbid: "mbid-27", xref: map[domain.ProviderKey]string{"deezer": "27"}, found: true}
	store := cache.NewRedisIdentityStore(inner, client)

	store.LookupByProviderID(context.Background(), domain.ResultKindArtist, domain.ProviderKeyDeezer, "27")

	assertStored(t, server, deezerArtist27IdentityKey, 30*24*time.Hour)
	if got, _ := server.Get(deezerArtist27IdentityKey); got != `{"mbid":"mbid-27","xref":{"deezer":"27"}}` {
		t.Errorf("stored entry = %s, want the mbid and xref", got)
	}
}

func TestRedisIdentityStore_ReadsACachedEntryWrittenUnderTheV1Key(t *testing.T) {
	server, client := newRedis(t)
	if err := server.Set("discovery:identity:v1:album:5c60b11e04600784205a0994464645d1",
		`{"mbid":"mbid-itunes","xref":{"itunes":"1440818839"}}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	inner := &recordingIdentityStore{}
	store := cache.NewRedisIdentityStore(inner, client)

	mbid, xref, ok := store.LookupByProviderID(context.Background(), domain.ResultKindAlbum, domain.ProviderKeyITunes, "1440818839")
	if !ok || mbid != "mbid-itunes" || xref["itunes"] != "1440818839" {
		t.Errorf("lookup = (%q,%v,%v), want the cached entry", mbid, xref, ok)
	}
	if inner.calls() != 0 {
		t.Errorf("durable lookups = %d, want 0 for a cached entry", inner.calls())
	}
}

func TestRedisIdentityStore_ACachedEntryWithoutAnMBIDFallsThroughToTheDurableStore(t *testing.T) {
	server, client := newRedis(t)
	if err := server.Set(deezerArtist27IdentityKey, `{"mbid":"","xref":{"deezer":"27"}}`); err != nil {
		t.Fatalf("seed: %v", err)
	}
	inner := &recordingIdentityStore{mbid: "mbid-27", found: true}
	store := cache.NewRedisIdentityStore(inner, client)

	mbid, _, ok := store.LookupByProviderID(context.Background(), domain.ResultKindArtist, domain.ProviderKeyDeezer, "27")
	if !ok || mbid != "mbid-27" || inner.calls() != 1 {
		t.Errorf("lookup = (%q,%v) after %d durable lookups, want the durable mbid-27 after 1", mbid, ok, inner.calls())
	}
}

func TestRedisIdentityStore_AnEmptyProviderOrIDIsAMissWithoutALookup(t *testing.T) {
	_, client := newRedis(t)
	inner := &recordingIdentityStore{mbid: "mbid-1", found: true}
	store := cache.NewRedisIdentityStore(inner, client)
	ctx := context.Background()

	if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "", "27"); ok {
		t.Error("an empty provider hit, want a miss")
	}
	if _, _, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, domain.ProviderKeyDeezer, ""); ok {
		t.Error("an empty external id hit, want a miss")
	}
	if inner.calls() != 0 {
		t.Errorf("durable lookups = %d, want 0", inner.calls())
	}
}

func TestRedisIdentityStore_NilClient_DelegatesToInner(t *testing.T) {
	inner := &recordingIdentityStore{
		mbid: "mbid-1", xref: map[domain.ProviderKey]string{"deezer": "9"}, found: true,
	}
	store := cache.NewRedisIdentityStore(inner, nil)

	mbid, xref, ok := store.LookupByProviderID(context.Background(), domain.ResultKindArtist, "deezer", "9")
	if !ok || mbid != "mbid-1" || xref["deezer"] != "9" {
		t.Errorf("nil-client lookup = (%q,%v,%v), want durable-store value", mbid, xref, ok)
	}
	if inner.calls() != 1 {
		t.Errorf("durable lookup calls = %d, want 1", inner.calls())
	}
}

func TestRedisIdentityStore_Lookup_AFailedBackfillIsLoggedAndStillAnswers(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	t.Cleanup(func() { slog.SetDefault(prev) })
	inner := &recordingIdentityStore{mbid: "mbid-1", xref: map[domain.ProviderKey]string{"deezer": "9"}, found: true}
	store := cache.NewRedisIdentityStore(inner, unreachableRedisClient(t))

	mbid, _, ok := store.LookupByProviderID(context.Background(), domain.ResultKindArtist, "deezer", "9")

	if !ok || mbid != "mbid-1" {
		t.Fatalf("lookup = (%q, %v), want the durable answer although Redis is down", mbid, ok)
	}
	if !strings.Contains(logs.String(), "identity.cache_backfill_failed") {
		t.Errorf("failed backfill not logged: %q", logs.String())
	}
}
