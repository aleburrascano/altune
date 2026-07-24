package cache

import (
	"context"
	"fmt"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// The upgrade direction of the overwrite guard: a provisional name-resolved
// image is REPLACED once a proven-identity image arrives.
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

// A negative (no-artwork) entry must not block a later real image: a name-level
// find after a cached miss overwrites the negative entry.
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

// Identity-aware keying: the same (title, subtitle) with and without an MBID
// are distinct entries — the same-name ("Che") fix. A per-name image must never
// be served for the identity-resolved entity or vice versa.
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

// Negative-cache TTL per kind, observed on the REAL stored key (not just the
// helper): a cached miss must expire on the per-kind schedule, and positive
// entries must outlive provisional ones.
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
