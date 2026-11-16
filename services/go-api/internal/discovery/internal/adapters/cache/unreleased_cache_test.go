package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"
)

func TestRedisUnreleasedCache_DisabledClientNeverHits(t *testing.T) {
	c := NewRedisUnreleasedCache(nil)
	ctx := context.Background()

	if err := c.Set(ctx, domain.ProviderDeezer, "1", "artist", []domain.UnreleasedSong{{GeniusID: 1}}, time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if _, ok := c.Get(ctx, domain.ProviderDeezer, "1", "artist"); ok {
		t.Error("Get hit on a disabled cache")
	}
}

func TestUnreleasedKey_DiffersByArtistName(t *testing.T) {
	if unreleasedKey(domain.ProviderDeezer, "1", "right name") == unreleasedKey(domain.ProviderDeezer, "1", "wrong name") {
		t.Error("two artist names share one cache key")
	}
}

func TestRedisUnreleasedCache_RoundTripsWithTheGivenTTL(t *testing.T) {
	client := testRedisClient(t)
	c := NewRedisUnreleasedCache(client)
	ctx := context.Background()
	id := fmt.Sprintf("round-trip-%s", t.Name())
	cleanKeys(t, client, unreleasedKey(domain.ProviderDeezer, id, "artist"))
	want := []domain.UnreleasedSong{{GeniusID: 5, Title: "Cancun", GeniusURL: "https://genius.com/c", HasMedia: new(true), AudioLinks: []domain.AudioLink{{Provider: "youtube", URL: "https://www.youtube.com/watch?v=dQw4w9WgXcQ"}}}}

	if err := c.Set(ctx, domain.ProviderDeezer, id, "artist", want, time.Hour); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, ok := c.Get(ctx, domain.ProviderDeezer, id, "artist")
	if !ok || !reflect.DeepEqual(got, want) {
		t.Errorf("Get = (%v, %v), want (%v, true)", got, ok, want)
	}
	if _, ok := c.Get(ctx, domain.ProviderDeezer, id, "other"); ok {
		t.Error("a different artist name hit the entry")
	}
	if ttl := client.TTL(ctx, unreleasedKey(domain.ProviderDeezer, id, "artist")).Val(); ttl <= 0 || ttl > time.Hour {
		t.Errorf("ttl = %v, want within (0, 1h]", ttl)
	}
}

func TestRedisUnreleasedCache_IgnoresEntriesWrittenUnderTheUnversionedKey(t *testing.T) {
	client := testRedisClient(t)
	c := NewRedisUnreleasedCache(client)
	ctx := context.Background()
	id := fmt.Sprintf("legacy-%s", t.Name())
	legacyKey := fmt.Sprintf("unreleased:songs:%s:%s:%s", domain.ProviderDeezer.Key(), id, "artist")
	cleanKeys(t, client, legacyKey, unreleasedKey(domain.ProviderDeezer, id, "artist"))
	if err := client.Set(ctx, legacyKey, `[{"genius_id":9,"title":"Released Single"}]`, time.Hour).Err(); err != nil {
		t.Fatalf("seed legacy entry: %v", err)
	}

	if songs, ok := c.Get(ctx, domain.ProviderDeezer, id, "artist"); ok {
		t.Errorf("Get served the legacy entry %v", songs)
	}
}

func TestUnreleasedKey_IsNotTheUnversionedLegacyKey(t *testing.T) {
	legacyKey := fmt.Sprintf("unreleased:songs:%s:%s:%s", domain.ProviderDeezer.Key(), "13631667", "don toliver")
	if unreleasedKey(domain.ProviderDeezer, "13631667", "don toliver") == legacyKey {
		t.Error("the key still matches the pre-fix cache entries")
	}
}
