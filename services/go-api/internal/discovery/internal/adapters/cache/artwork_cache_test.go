package cache_test

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/adapters/cache"
	"altune/go-api/internal/discovery/internal/core/ports"
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

const humbleArtworkKey = "discovery:artwork:v3:track:93e2a80d49992538aa72cde4bca801e2"

func TestRedisArtworkCache_StoresTheEntryUnderTheV3KeyAsCompactJSON(t *testing.T) {
	server, client := newRedis(t)
	c := cache.NewRedisArtworkCache(client)

	if err := c.Set(context.Background(), domain.ResultKindTrack, "Humble", "Kendrick Lamar", "mbid-1",
		"https://x/a.jpg", domain.ProviderKeyDiscogs, ports.ArtworkConfidenceIdentity); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if got := storedKeys(server); len(got) != 1 || got[0] != humbleArtworkKey {
		t.Fatalf("stored keys = %v, want only %q", got, humbleArtworkKey)
	}
	raw, err := server.Get(humbleArtworkKey)
	if err != nil {
		t.Fatalf("read stored entry: %v", err)
	}
	if want := `{"u":"https://x/a.jpg","s":"discogs","c":2}`; raw != want {
		t.Errorf("stored entry = %s, want %s", raw, want)
	}
}

func TestRedisArtworkCache_StoredTTLFollowsKindAndConfidence(t *testing.T) {
	tests := []struct {
		name string
		kind domain.ResultKind
		url  string
		conf ports.ArtworkConfidence
		want time.Duration
	}{
		{"a track without artwork rechecks soonest", domain.ResultKindTrack, "", ports.ArtworkConfidenceNone, 6 * time.Hour},
		{"an album without artwork rechecks at medium pace", domain.ResultKindAlbum, "", ports.ArtworkConfidenceNone, 12 * time.Hour},
		{"an artist without artwork rechecks slowest", domain.ResultKindArtist, "", ports.ArtworkConfidenceNone, 24 * time.Hour},
		{"an unknown kind without artwork rechecks conservatively", domain.ResultKindUnknown, "", ports.ArtworkConfidenceNone, 24 * time.Hour},
		{"an identity image is kept for two weeks", domain.ResultKindArtist, "https://img/x.jpg", ports.ArtworkConfidenceIdentity, 14 * 24 * time.Hour},
		{"a name-matched image is provisional", domain.ResultKindArtist, "https://img/y.jpg", ports.ArtworkConfidenceName, 48 * time.Hour},
		{"an unproven image is provisional", domain.ResultKindAlbum, "https://img/z.jpg", ports.ArtworkConfidenceNone, 48 * time.Hour},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := newRedis(t)
			c := cache.NewRedisArtworkCache(client)
			if err := c.Set(context.Background(), tt.kind, "Title", "Sub", "", tt.url, "src", tt.conf); err != nil {
				t.Fatalf("Set: %v", err)
			}
			keys := storedKeys(server)
			if len(keys) != 1 {
				t.Fatalf("stored keys = %v, want one", keys)
			}
			assertStored(t, server, keys[0], tt.want)
		})
	}
}

func TestRedisArtworkCache_SetAndGet_CacheHit(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisArtworkCache(client)
	ctx := context.Background()

	url := "https://example.com/artwork.jpg"
	if err := c.Set(ctx, domain.ResultKindAlbum, "Test Album", "Test Artist", "", url, "fanart", ports.ArtworkConfidenceIdentity); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, gotSource, hit, err := c.Get(ctx, domain.ResultKindAlbum, "Test Album", "Test Artist", "")
	if err != nil || !hit {
		t.Fatalf("Get = (hit=%v, err=%v), want hit", hit, err)
	}
	if got != url || gotSource != "fanart" {
		t.Errorf("Get = (%q,%q), want (%q,fanart)", got, gotSource, url)
	}
}

func TestRedisArtworkCache_KeyDependsOnKindTitleSubtitleAndMBID(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisArtworkCache(client)
	ctx := context.Background()

	if err := c.Set(ctx, domain.ResultKindAlbum, "Title", "Sub", "mbid", "https://img/1.jpg", "src", ports.ArtworkConfidenceName); err != nil {
		t.Fatalf("Set: %v", err)
	}
	for _, other := range []struct {
		name                  string
		kind                  domain.ResultKind
		title, subtitle, mbid string
	}{
		{"kind", domain.ResultKindTrack, "Title", "Sub", "mbid"},
		{"title", domain.ResultKindAlbum, "Other", "Sub", "mbid"},
		{"subtitle", domain.ResultKindAlbum, "Title", "Other", "mbid"},
		{"mbid", domain.ResultKindAlbum, "Title", "Sub", ""},
	} {
		if _, _, hit, _ := c.Get(ctx, other.kind, other.title, other.subtitle, other.mbid); hit {
			t.Errorf("a different %s hit the entry", other.name)
		}
	}
}

func TestRedisArtworkCache_SetEmpty_NegativeCache(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisArtworkCache(client)
	ctx := context.Background()

	if err := c.Set(ctx, domain.ResultKindTrack, "No Artwork", "Unknown", "", "", "", ports.ArtworkConfidenceNone); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, _, hit, err := c.Get(ctx, domain.ResultKindTrack, "No Artwork", "Unknown", "")
	if err != nil || !hit {
		t.Fatalf("Get = (hit=%v, err=%v), want a hit for the negative entry", hit, err)
	}
	if got != "" {
		t.Errorf("negative entry URL = %q, want empty", got)
	}
}

func TestRedisArtworkCache_Get_CacheMiss(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisArtworkCache(client)

	got, source, hit, err := c.Get(context.Background(), domain.ResultKindArtist, "Nonexistent", "nobody", "")
	if err != nil || hit || got != "" || source != "" {
		t.Errorf("Get = (%q,%q,%v,%v), want a clean miss", got, source, hit, err)
	}
}

func TestRedisArtworkCache_NameThenIdentity_Upgrades(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisArtworkCache(client)
	ctx := context.Background()
	kind := domain.ResultKindArtist

	if err := c.Set(ctx, kind, "Upgrade Artist", "", "mbid-upgrade", "https://name/guess.jpg", "deezer", ports.ArtworkConfidenceName); err != nil {
		t.Fatalf("name set: %v", err)
	}
	if err := c.Set(ctx, kind, "Upgrade Artist", "", "mbid-upgrade", "https://caa/proven.jpg", "caa", ports.ArtworkConfidenceIdentity); err != nil {
		t.Fatalf("identity set: %v", err)
	}

	got, gotSource, hit, _ := c.Get(ctx, kind, "Upgrade Artist", "", "mbid-upgrade")
	if !hit || got != "https://caa/proven.jpg" || gotSource != "caa" {
		t.Errorf("after upgrade Get = (%q,%q,%v), want the identity image from caa", got, gotSource, hit)
	}
}

func TestRedisArtworkCache_NegativeThenName_Overwrites(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisArtworkCache(client)
	ctx := context.Background()
	kind := domain.ResultKindTrack

	if err := c.Set(ctx, kind, "Late Artwork", "artist", "", "", "", ports.ArtworkConfidenceNone); err != nil {
		t.Fatalf("negative set: %v", err)
	}
	if err := c.Set(ctx, kind, "Late Artwork", "artist", "", "https://late/img.jpg", "itunes", ports.ArtworkConfidenceName); err != nil {
		t.Fatalf("name set: %v", err)
	}
	if got, _, _, _ := c.Get(ctx, kind, "Late Artwork", "artist", ""); got != "https://late/img.jpg" {
		t.Errorf("negative entry blocked the later image: got %q", got)
	}
}

func TestRedisArtworkCache_AnEntryWithoutAnImageNeverBlocksALowerConfidenceImage(t *testing.T) {
	server, client := newRedis(t)
	c := cache.NewRedisArtworkCache(client)
	if err := server.Set(humbleArtworkKey, `{"u":"","s":"","c":2}`); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	if err := c.Set(context.Background(), domain.ResultKindTrack, "Humble", "Kendrick Lamar", "mbid-1",
		"https://name/guess.jpg", "deezer", ports.ArtworkConfidenceName); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, _, _, _ := c.Get(context.Background(), domain.ResultKindTrack, "Humble", "Kendrick Lamar", "mbid-1"); got != "https://name/guess.jpg" {
		t.Errorf("an imageless entry blocked the image: got %q", got)
	}
}

func TestRedisArtworkCache_ACorruptEntryIsOverwritten(t *testing.T) {
	server, client := newRedis(t)
	c := cache.NewRedisArtworkCache(client)
	if err := server.Set(humbleArtworkKey, "{not json"); err != nil {
		t.Fatalf("seed entry: %v", err)
	}

	if err := c.Set(context.Background(), domain.ResultKindTrack, "Humble", "Kendrick Lamar", "mbid-1",
		"https://name/guess.jpg", "deezer", ports.ArtworkConfidenceNone); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, _, _, _ := c.Get(context.Background(), domain.ResultKindTrack, "Humble", "Kendrick Lamar", "mbid-1"); got != "https://name/guess.jpg" {
		t.Errorf("a corrupt entry blocked the image: got %q", got)
	}
}

func TestRedisArtworkCache_SetReturnsTheErrorWhenTheExistingEntryCannotBeRead(t *testing.T) {
	server, client := newRedis(t)
	c := cache.NewRedisArtworkCache(client)
	if _, err := server.Lpush(humbleArtworkKey, "not a string"); err != nil {
		t.Fatalf("seed wrong type: %v", err)
	}

	err := c.Set(context.Background(), domain.ResultKindTrack, "Humble", "Kendrick Lamar", "mbid-1",
		"https://name/guess.jpg", "deezer", ports.ArtworkConfidenceIdentity)
	if err == nil {
		t.Fatal("Set overwrote an entry it could not read, want the read error")
	}
}

func TestRedisArtworkCache_IdentityNotOverwrittenByName(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisArtworkCache(client)
	ctx := context.Background()
	kind, title, mbid := domain.ResultKindArtist, "Guarded Artist", "mbid-guard"

	identityURL := "https://caa/identity.jpg"
	if err := c.Set(ctx, kind, title, "", mbid, identityURL, "discogs", ports.ArtworkConfidenceIdentity); err != nil {
		t.Fatalf("seed identity: %v", err)
	}
	if err := c.Set(ctx, kind, title, "", mbid, "https://name/guess.jpg", "deezer", ports.ArtworkConfidenceName); err != nil {
		t.Fatalf("name set: %v", err)
	}
	got, gotSource, hit, _ := c.Get(ctx, kind, title, "", mbid)
	if !hit || got != identityURL || gotSource != "discogs" {
		t.Errorf("identity image was overwritten: got (%q,%q), want (%q,discogs)", got, gotSource, identityURL)
	}

	if err := c.Set(ctx, kind, title, "", mbid, "", "", ports.ArtworkConfidenceNone); err != nil {
		t.Fatalf("negative set: %v", err)
	}
	if got, _, _, _ := c.Get(ctx, kind, title, "", mbid); got != identityURL {
		t.Errorf("identity image wiped by a later failure: got %q, want %q", got, identityURL)
	}

	newIdentityURL := "https://caa/identity-v2.jpg"
	if err := c.Set(ctx, kind, title, "", mbid, newIdentityURL, "caa", ports.ArtworkConfidenceIdentity); err != nil {
		t.Fatalf("identity refresh: %v", err)
	}
	if got, _, _, _ := c.Get(ctx, kind, title, "", mbid); got != newIdentityURL {
		t.Errorf("equal-confidence refresh failed: got %q, want %q", got, newIdentityURL)
	}
}

func TestRedisArtworkCache_ConcurrentIdentityAndNameKeepsIdentity(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisArtworkCache(client)
	ctx := context.Background()
	kind, title, mbid := domain.ResultKindArtist, "Raced Artist", "mbid-race"

	identityURL := "https://caa/identity.jpg"
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Go(func() {
			if i%2 == 0 {
				_ = c.Set(ctx, kind, title, "", mbid, identityURL, "discogs", ports.ArtworkConfidenceIdentity)
				return
			}
			_ = c.Set(ctx, kind, title, "", mbid, fmt.Sprintf("https://name/%d.jpg", i), "deezer", ports.ArtworkConfidenceName)
		})
	}
	wg.Wait()

	got, source, hit, _ := c.Get(ctx, kind, title, "", mbid)
	if !hit || got != identityURL || source != "discogs" {
		t.Errorf("after racing sets got (%q,%q,%v), want identity entry (%q,discogs)", got, source, hit, identityURL)
	}
}

func TestRedisArtworkCache_NilClient_NoOps(t *testing.T) {
	c := cache.NewRedisArtworkCache(nil)
	ctx := context.Background()

	url, source, hit, err := c.Get(ctx, domain.ResultKindTrack, "t", "s", "")
	if url != "" || source != "" || hit || err != nil {
		t.Errorf("nil-client Get = (%q,%q,%v,%v), want clean miss", url, source, hit, err)
	}
	if err := c.Set(ctx, domain.ResultKindTrack, "t", "s", "", "u", "src", ports.ArtworkConfidenceName); err != nil {
		t.Errorf("nil-client Set must no-op, got %v", err)
	}
}
