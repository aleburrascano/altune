package cache

import (
	"context"
	"fmt"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestRedisResultCache_RoundTripAndFreshCopies(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisResultCache(client)
	ctx := context.Background()

	key := fmt.Sprintf("qa-results|%s", t.Name())
	cleanKeys(t, client, resultCacheKey(key))

	original := []domain.SearchResult{
		{
			Kind:     domain.ResultKindTrack,
			Title:    "Cached Track",
			Subtitle: "Cached Artist",
			ImageURL: "https://img/1.jpg",
			MBID:     "mbid-1",
			Xref:     map[string]string{"deezer": "42"},
			Sources: []domain.SourceRef{
				{Provider: domain.ProviderDeezer, ExternalID: "42", URL: "https://deezer/42"},
			},
			Album:    "Cached Album",
			Duration: 200,
		},
		{
			Kind:  domain.ResultKindArtist,
			Title: "Cached Artist",
		},
	}
	cache.Set(ctx, key, original)

	got, hit := cache.Get(ctx, key)
	if !hit {
		t.Fatal("expected hit after Set")
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	first := got[0]
	if first.Title != "Cached Track" || first.Subtitle != "Cached Artist" ||
		first.Kind != domain.ResultKindTrack || first.MBID != "mbid-1" ||
		first.Album != "Cached Album" || first.Duration != 200 {
		t.Errorf("first result did not round-trip: %+v", first)
	}
	if first.Xref["deezer"] != "42" {
		t.Errorf("Xref did not round-trip: %v", first.Xref)
	}
	if len(first.Sources) != 1 || first.Sources[0].ExternalID != "42" {
		t.Errorf("Sources did not round-trip: %v", first.Sources)
	}

	// Deep-copy semantics: mutating what Get returned (fields AND nested maps)
	// must not bleed into the next Get — the cache serves fresh copies, not a
	// shared slice one caller can corrupt for every other request.
	got[0].Title = "MUTATED"
	got[0].Xref["deezer"] = "corrupted"

	again, hit := cache.Get(ctx, key)
	if !hit {
		t.Fatal("expected hit on second Get")
	}
	if again[0].Title != "Cached Track" {
		t.Errorf("mutation of a returned result leaked into the cache: title = %q", again[0].Title)
	}
	if again[0].Xref["deezer"] != "42" {
		t.Errorf("mutation of a returned nested map leaked into the cache: xref = %v", again[0].Xref)
	}
}

func TestRedisResultCache_KeyIsolationAndMiss(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisResultCache(client)
	ctx := context.Background()

	keyA := fmt.Sprintf("qa-results-a|%s", t.Name())
	keyB := fmt.Sprintf("qa-results-b|%s", t.Name())
	cleanKeys(t, client, resultCacheKey(keyA), resultCacheKey(keyB))

	cache.Set(ctx, keyA, []domain.SearchResult{{Title: "A"}})

	if _, hit := cache.Get(ctx, keyB); hit {
		t.Error("a different composite key must miss, got hit")
	}
	if _, hit := cache.Get(ctx, "qa-results-never-set|"+t.Name()); hit {
		t.Error("never-set key must miss, got hit")
	}
}

// A corrupt stored value is a miss (so the search recomputes and overwrites),
// never an error or a poisoned decode.
func TestRedisResultCache_CorruptValueIsMiss(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisResultCache(client)
	ctx := context.Background()

	key := fmt.Sprintf("qa-results-corrupt|%s", t.Name())
	redisKey := resultCacheKey(key)
	cleanKeys(t, client, redisKey)

	if err := client.Set(ctx, redisKey, "{not json[", 0).Err(); err != nil {
		t.Fatalf("seed corrupt value: %v", err)
	}
	if got, hit := cache.Get(ctx, key); hit || got != nil {
		t.Errorf("corrupt value Get = (%v,%v), want clean miss", got, hit)
	}
}
