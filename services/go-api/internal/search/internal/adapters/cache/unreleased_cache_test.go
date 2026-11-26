package cache_test

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"reflect"
	"testing"
	"time"
)

func TestRedisUnreleasedCache_DisabledClientNeverHits(t *testing.T) {
	c := cache.NewRedisUnreleasedCache(nil)
	ctx := context.Background()

	if err := c.Set(ctx, domain.ProviderDeezer, "1", "artist", []domain.UnreleasedSong{{GeniusID: 1}}, time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, ok := c.Get(ctx, domain.ProviderDeezer, "1", "artist"); ok {
		t.Error("Get hit on a disabled cache")
	}
}

func TestRedisUnreleasedCache_RoundTripsUnderTheV9KeyWithTheGivenTTL(t *testing.T) {
	server, client := newRedis(t)
	c := cache.NewRedisUnreleasedCache(client)
	ctx := context.Background()
	want := []domain.UnreleasedSong{{GeniusID: 5, Title: "Cancun", GeniusURL: "https://genius.com/c", HasMedia: new(true), AudioLinks: []domain.AudioLink{{Provider: "youtube", URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}}}}

	if err := c.Set(ctx, domain.ProviderDeezer, "13631667", "don toliver", want, time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, ok := c.Get(ctx, domain.ProviderDeezer, "13631667", "don toliver")
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("Get = (%v, %v), want (%v, true)", got, ok, want)
	}
	assertStored(t, server, "unreleased:songs:v9:deezer:13631667:don toliver", time.Hour)
}

func TestRedisUnreleasedCache_KeyDependsOnProviderIDAndArtistName(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisUnreleasedCache(client)
	ctx := context.Background()
	if err := c.Set(ctx, domain.ProviderDeezer, "1", "right name", []domain.UnreleasedSong{{GeniusID: 1}}, time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}

	if _, ok := c.Get(ctx, domain.ProviderDeezer, "1", "wrong name"); ok {
		t.Error("a different artist name hit the entry")
	}
	if _, ok := c.Get(ctx, domain.ProviderDeezer, "2", "right name"); ok {
		t.Error("a different external id hit the entry")
	}
	if _, ok := c.Get(ctx, domain.ProviderSpotify, "1", "right name"); ok {
		t.Error("a different provider hit the entry")
	}
}

func TestRedisUnreleasedCache_IgnoresEntriesWrittenUnderTheUnversionedKey(t *testing.T) {
	server, client := newRedis(t)
	c := cache.NewRedisUnreleasedCache(client)
	if err := server.Set("unreleased:songs:deezer:13631667:don toliver", `[{"genius_id":9,"title":"Released Single"}]`); err != nil {
		t.Fatalf("seed legacy entry: %v", err)
	}

	if songs, ok := c.Get(context.Background(), domain.ProviderDeezer, "13631667", "don toliver"); ok {
		t.Errorf("Get served the legacy entry %v", songs)
	}
}
