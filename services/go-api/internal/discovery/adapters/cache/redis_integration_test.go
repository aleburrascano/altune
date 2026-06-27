package cache

import (
	"context"
	"fmt"
	"os"
	"testing"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

// --- helpers ----------------------------------------------------------------

func testRedisClient(t *testing.T) *goredis.Client {
	t.Helper()
	url := os.Getenv("REDIS_URL")
	if url == "" {
		t.Skip("REDIS_URL not set, skipping Redis integration test")
	}
	opts, err := goredis.ParseURL(url)
	if err != nil {
		t.Fatalf("parse REDIS_URL: %v", err)
	}
	client := goredis.NewClient(opts)
	t.Cleanup(func() { client.Close() })
	return client
}

// cleanKeys deletes all keys matching a pattern scoped to the test.
func cleanKeys(t *testing.T, client *goredis.Client, keys ...string) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		for _, k := range keys {
			client.Del(ctx, k)
		}
	})
}

// --- RedisArtworkCache ------------------------------------------------------

func TestRedisArtworkCache_SetAndGet_CacheHit(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisArtworkCache(client)
	ctx := context.Background()

	// Arrange
	kind := domain.ResultKindAlbum
	title := fmt.Sprintf("Test Album %s", t.Name())
	subtitle := "Test Artist"
	mbid := ""
	url := "https://example.com/artwork.jpg"

	key := artworkCacheKey(kind, title, subtitle, mbid)
	cleanKeys(t, client, key)

	// Act
	err := cache.Set(ctx, kind, title, subtitle, mbid, url, "fanart")
	if err != nil {
		t.Fatalf("Set returned unexpected error: %v", err)
	}

	got, gotSource, hit, err := cache.Get(ctx, kind, title, subtitle, mbid)

	// Assert
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

	// Arrange — set empty URL (negative cache)
	kind := domain.ResultKindTrack
	title := fmt.Sprintf("No Artwork %s", t.Name())
	subtitle := "Unknown"
	mbid := ""

	key := artworkCacheKey(kind, title, subtitle, mbid)
	cleanKeys(t, client, key)

	// Act
	err := cache.Set(ctx, kind, title, subtitle, mbid, "", "")
	if err != nil {
		t.Fatalf("Set returned unexpected error: %v", err)
	}

	got, _, hit, err := cache.Get(ctx, kind, title, subtitle, mbid)

	// Assert
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

func TestRedisArtworkCache_Get_CacheMiss(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisArtworkCache(client)
	ctx := context.Background()

	// Arrange — keys that were never set
	kind := domain.ResultKindArtist
	title := fmt.Sprintf("Nonexistent %s", t.Name())

	// Act
	got, _, hit, err := cache.Get(ctx, kind, title, "nobody", "")

	// Assert
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
