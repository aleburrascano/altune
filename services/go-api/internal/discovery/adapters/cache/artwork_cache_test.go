package cache

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func TestNegativeTTL_PerKind(t *testing.T) {
	tests := []struct {
		name string
		kind domain.ResultKind
		want time.Duration
	}{
		{"track churns most, rechecks soonest", domain.ResultKindTrack, 6 * time.Hour},
		{"album medium churn", domain.ResultKindAlbum, 12 * time.Hour},
		{"artist most stable", domain.ResultKindArtist, 24 * time.Hour},
		{"unknown kind defaults conservative", domain.ResultKind(0), 24 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := negativeTTL(tt.kind); got != tt.want {
				t.Errorf("negativeTTL(%v) = %v, want %v", tt.kind, got, tt.want)
			}
		})
	}
	for _, k := range []domain.ResultKind{domain.ResultKindTrack, domain.ResultKindAlbum, domain.ResultKindArtist} {
		if negativeTTL(k) >= artworkPositiveTTL {
			t.Errorf("negativeTTL(%v) must be shorter than positive TTL %v", k, artworkPositiveTTL)
		}
	}
}

func TestRedisArtworkCache_NameThenIdentity_Upgrades(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisArtworkCache(client)
	ctx := context.Background()

	kind := domain.ResultKindArtist
	title := fmt.Sprintf("Upgrade Artist %s", t.Name())
	key := artworkCacheKey(kind, title, "", "mbid-upgrade")
	cleanKeys(t, client, key)

	if err := cache.Set(ctx, kind, title, "", "mbid-upgrade",
		"https://name/guess.jpg", "deezer", ports.ArtworkConfidenceName); err != nil {
		t.Fatalf("name set: %v", err)
	}
	if err := cache.Set(ctx, kind, title, "", "mbid-upgrade",
		"https://caa/proven.jpg", "caa", ports.ArtworkConfidenceIdentity); err != nil {
		t.Fatalf("identity set: %v", err)
	}

	got, gotSource, hit, _ := cache.Get(ctx, kind, title, "", "mbid-upgrade")
	if !hit || got != "https://caa/proven.jpg" || gotSource != "caa" {
		t.Errorf("after upgrade Get = (%q,%q,%v), want the identity image from caa", got, gotSource, hit)
	}
}

func TestRedisArtworkCache_NegativeThenName_Overwrites(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisArtworkCache(client)
	ctx := context.Background()

	kind := domain.ResultKindTrack
	title := fmt.Sprintf("Late Artwork %s", t.Name())
	key := artworkCacheKey(kind, title, "artist", "")
	cleanKeys(t, client, key)

	if err := cache.Set(ctx, kind, title, "artist", "", "", "", ports.ArtworkConfidenceNone); err != nil {
		t.Fatalf("negative set: %v", err)
	}
	if err := cache.Set(ctx, kind, title, "artist", "",
		"https://late/img.jpg", "itunes", ports.ArtworkConfidenceName); err != nil {
		t.Fatalf("name set: %v", err)
	}
	if got, _, _, _ := cache.Get(ctx, kind, title, "artist", ""); got != "https://late/img.jpg" {
		t.Errorf("negative entry blocked the later image: got %q", got)
	}
}

func TestRedisArtworkCache_MBIDKeySeparation(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisArtworkCache(client)
	ctx := context.Background()

	kind := domain.ResultKindArtist
	title := fmt.Sprintf("Same Name %s", t.Name())
	keyNoMBID := artworkCacheKey(kind, title, "", "")
	keyMBID := artworkCacheKey(kind, title, "", "mbid-che-1")
	cleanKeys(t, client, keyNoMBID, keyMBID)

	if err := cache.Set(ctx, kind, title, "", "",
		"https://name/keyed.jpg", "deezer", ports.ArtworkConfidenceName); err != nil {
		t.Fatalf("no-mbid set: %v", err)
	}
	if err := cache.Set(ctx, kind, title, "", "mbid-che-1",
		"https://identity/keyed.jpg", "fanart", ports.ArtworkConfidenceIdentity); err != nil {
		t.Fatalf("mbid set: %v", err)
	}

	if got, _, hit, _ := cache.Get(ctx, kind, title, "", ""); !hit || got != "https://name/keyed.jpg" {
		t.Errorf("empty-mbid entry = (%q,%v), want the name-keyed image", got, hit)
	}
	if got, _, hit, _ := cache.Get(ctx, kind, title, "", "mbid-che-1"); !hit || got != "https://identity/keyed.jpg" {
		t.Errorf("mbid entry = (%q,%v), want the identity-keyed image", got, hit)
	}
}

func TestRedisArtworkCache_StoredTTLs(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisArtworkCache(client)
	ctx := context.Background()

	ttlOf := func(t *testing.T, kind domain.ResultKind, title, mbid, url string, conf ports.ArtworkConfidence) time.Duration {
		t.Helper()
		key := artworkCacheKey(kind, title, "sub", mbid)
		cleanKeys(t, client, key)
		if err := cache.Set(ctx, kind, title, "sub", mbid, url, "src", conf); err != nil {
			t.Fatalf("Set: %v", err)
		}
		ttl, err := client.TTL(ctx, key).Result()
		if err != nil {
			t.Fatalf("TTL: %v", err)
		}
		return ttl
	}

	within := func(got, want time.Duration) bool {
		return got > want-time.Minute && got <= want
	}

	tests := []struct {
		name string
		kind domain.ResultKind
		url  string
		conf ports.ArtworkConfidence
		want time.Duration
	}{
		{"negative track expires soonest", domain.ResultKindTrack, "", ports.ArtworkConfidenceNone, artworkNegativeTTLTrack},
		{"negative album medium", domain.ResultKindAlbum, "", ports.ArtworkConfidenceNone, artworkNegativeTTLAlbum},
		{"negative artist longest", domain.ResultKindArtist, "", ports.ArtworkConfidenceNone, artworkNegativeTTLArtist},
		{"identity image near-permanent", domain.ResultKindArtist, "https://img/x.jpg", ports.ArtworkConfidenceIdentity, artworkPositiveTTL},
		{"name image provisional", domain.ResultKindArtist, "https://img/y.jpg", ports.ArtworkConfidenceName, artworkProvisionalTTL},
	}
	for i, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			title := fmt.Sprintf("TTL %d %s", i, t.Name())
			if got := ttlOf(t, tt.kind, title, "", tt.url, tt.conf); !within(got, tt.want) {
				t.Errorf("stored TTL = %v, want ~%v", got, tt.want)
			}
		})
	}
}

func TestRedisArtworkCache_SetAndGet_CacheHit(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisArtworkCache(client)
	ctx := context.Background()

	kind := domain.ResultKindAlbum
	title := fmt.Sprintf("Test Album %s", t.Name())
	subtitle := "Test Artist"
	mbid := ""
	url := "https://example.com/artwork.jpg"

	key := artworkCacheKey(kind, title, subtitle, mbid)
	cleanKeys(t, client, key)

	err := cache.Set(ctx, kind, title, subtitle, mbid, url, "fanart", ports.ArtworkConfidenceIdentity)
	if err != nil {
		t.Fatalf("Set returned unexpected error: %v", err)
	}

	got, gotSource, hit, err := cache.Get(ctx, kind, title, subtitle, mbid)
	if err != nil {
		t.Fatalf("Get returned unexpected error: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit, got miss")
	}
	if got != url {
		t.Errorf("expected URL %q, got %q", url, got)
	}
	if gotSource != "fanart" {
		t.Errorf("expected source %q to round-trip, got %q", "fanart", gotSource)
	}
}

func TestRedisArtworkCache_SetEmpty_NegativeCache(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisArtworkCache(client)
	ctx := context.Background()

	kind := domain.ResultKindTrack
	title := fmt.Sprintf("No Artwork %s", t.Name())
	subtitle := "Unknown"
	mbid := ""

	key := artworkCacheKey(kind, title, subtitle, mbid)
	cleanKeys(t, client, key)

	err := cache.Set(ctx, kind, title, subtitle, mbid, "", "", ports.ArtworkConfidenceNone)
	if err != nil {
		t.Fatalf("Set returned unexpected error: %v", err)
	}

	got, _, hit, err := cache.Get(ctx, kind, title, subtitle, mbid)
	if err != nil {
		t.Fatalf("Get returned unexpected error: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit for negative entry, got miss")
	}
	if got != "" {
		t.Errorf("expected empty URL for negative cache entry, got %q", got)
	}
}

func TestRedisArtworkCache_IdentityNotOverwrittenByName(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisArtworkCache(client)
	ctx := context.Background()

	kind := domain.ResultKindArtist
	title := fmt.Sprintf("Guarded Artist %s", t.Name())
	subtitle := ""
	mbid := "mbid-guard"
	key := artworkCacheKey(kind, title, subtitle, mbid)
	cleanKeys(t, client, key)

	identityURL := "https://caa/identity.jpg"
	if err := cache.Set(ctx, kind, title, subtitle, mbid, identityURL, "discogs", ports.ArtworkConfidenceIdentity); err != nil {
		t.Fatalf("seed identity: %v", err)
	}

	if err := cache.Set(ctx, kind, title, subtitle, mbid, "https://name/guess.jpg", "deezer", ports.ArtworkConfidenceName); err != nil {
		t.Fatalf("name set: %v", err)
	}
	got, gotSource, hit, _ := cache.Get(ctx, kind, title, subtitle, mbid)
	if !hit || got != identityURL || gotSource != "discogs" {
		t.Errorf("identity image was overwritten: got (%q,%q), want (%q,discogs)", got, gotSource, identityURL)
	}

	if err := cache.Set(ctx, kind, title, subtitle, mbid, "", "", ports.ArtworkConfidenceNone); err != nil {
		t.Fatalf("negative set: %v", err)
	}
	if got, _, _, _ := cache.Get(ctx, kind, title, subtitle, mbid); got != identityURL {
		t.Errorf("identity image wiped by a later failure: got %q, want %q", got, identityURL)
	}

	newIdentityURL := "https://caa/identity-v2.jpg"
	if err := cache.Set(ctx, kind, title, subtitle, mbid, newIdentityURL, "caa", ports.ArtworkConfidenceIdentity); err != nil {
		t.Fatalf("identity refresh: %v", err)
	}
	if got, _, _, _ := cache.Get(ctx, kind, title, subtitle, mbid); got != newIdentityURL {
		t.Errorf("equal-confidence refresh failed: got %q, want %q", got, newIdentityURL)
	}
}

func TestRedisArtworkCache_Get_CacheMiss(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisArtworkCache(client)
	ctx := context.Background()

	kind := domain.ResultKindArtist
	title := fmt.Sprintf("Nonexistent %s", t.Name())

	got, _, hit, err := cache.Get(ctx, kind, title, "nobody", "")
	if err != nil {
		t.Fatalf("Get returned unexpected error: %v", err)
	}
	if hit {
		t.Fatal("expected cache miss, got hit")
	}
	if got != "" {
		t.Errorf("expected empty string on cache miss, got %q", got)
	}
}

func TestRedisArtworkCache_NilClient_NoOps(t *testing.T) {
	c := NewRedisArtworkCache(nil)
	ctx := context.Background()

	url, source, hit, err := c.Get(ctx, domain.ResultKindTrack, "t", "s", "")
	if url != "" || source != "" || hit || err != nil {
		t.Errorf("nil-client Get = (%q,%q,%v,%v), want clean miss", url, source, hit, err)
	}
	if err := c.Set(ctx, domain.ResultKindTrack, "t", "s", "", "u", "src", ports.ArtworkConfidenceName); err != nil {
		t.Errorf("nil-client Set must no-op, got %v", err)
	}
}

func TestArtworkEntry_SourceSerializedAsBareString(t *testing.T) {
	entry := artworkEntry{URL: "https://x/a.jpg", Source: domain.ProviderKeyDiscogs.String(), Confidence: 2}
	b, err := json.Marshal(entry)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(b), `{"u":"https://x/a.jpg","s":"discogs","c":2}`; got != want {
		t.Errorf("artworkEntry JSON = %s, want %s", got, want)
	}
	if got, want := artworkCacheKey(domain.ResultKindTrack, "Humble", "Kendrick Lamar", "mbid-1"), "discovery:artwork:v3:track:93e2a80d49992538aa72cde4bca801e2"; got != want {
		t.Errorf("artworkCacheKey = %q, want %q", got, want)
	}
}
