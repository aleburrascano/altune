package cache

import (
	"context"
	"fmt"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestRedisNameKeyedCache_PositiveRoundTrip(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisDeezerEnrichmentCache(client)
	ctx := context.Background()

	nameKey := fmt.Sprintf("qa-nk|%s", t.Name())
	cleanKeys(t, client,
		hashKey(cache.posPrefix, nameKey),
		hashKey(cache.negPrefix, nameKey),
	)

	in := domain.DeezerEnrichment{
		BPM: 92, Gain: -7.1, Explicit: true,
		Label: "Top Dawg", Genres: []string{"Rap/Hip Hop"},
		UPC: "00602547311009", RecordType: "album",
	}
	if err := cache.Set(ctx, nameKey, in); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, hit, err := cache.Get(ctx, nameKey)
	if err != nil || !hit {
		t.Fatalf("Get = (hit=%v, err=%v), want hit", hit, err)
	}
	if got.BPM != 92 || got.Gain != -7.1 || !got.Explicit ||
		got.Label != "Top Dawg" || got.UPC != "00602547311009" || got.RecordType != "album" {
		t.Errorf("enrichment did not round-trip: %+v", got)
	}
	if len(got.Genres) != 1 || got.Genres[0] != "Rap/Hip Hop" {
		t.Errorf("genres did not round-trip: %v", got.Genres)
	}

	// A positive entry must not read as negative, and a different key misses.
	if neg, _ := cache.GetNegative(ctx, nameKey); neg {
		t.Error("positive entry reported negative")
	}
	if _, hit, _ := cache.Get(ctx, nameKey+"-other"); hit {
		t.Error("different name key hit, want miss")
	}
}

func TestRedisNameKeyedCache_NegativePath(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisLastFmEnrichmentCache(client)
	ctx := context.Background()

	nameKey := fmt.Sprintf("qa-nkneg|%s", t.Name())
	cleanKeys(t, client,
		hashKey(cache.posPrefix, nameKey),
		hashKey(cache.negPrefix, nameKey),
	)

	if err := cache.SetNegative(ctx, nameKey); err != nil {
		t.Fatalf("SetNegative: %v", err)
	}
	if neg, err := cache.GetNegative(ctx, nameKey); !neg || err != nil {
		t.Errorf("GetNegative = (%v,%v), want (true,nil)", neg, err)
	}
	// The negative marker must never surface through the positive path.
	if _, hit, _ := cache.Get(ctx, nameKey); hit {
		t.Error("negative marker readable as a positive entry")
	}
}

// The same name key cached by two providers must stay two entries: writing
// Deezer data must not make the Discogs cache hit.
func TestRedisNameKeyedCache_CrossProviderIsolation(t *testing.T) {
	client := testRedisClient(t)
	deezer := NewRedisDeezerEnrichmentCache(client)
	discogs := NewRedisDiscogsEnrichmentCache(client)
	ctx := context.Background()

	nameKey := fmt.Sprintf("qa-nkiso|%s", t.Name())
	cleanKeys(t, client,
		hashKey(deezer.posPrefix, nameKey),
		hashKey(discogs.posPrefix, nameKey),
	)

	if err := deezer.Set(ctx, nameKey, domain.DeezerEnrichment{Label: "only deezer"}); err != nil {
		t.Fatalf("deezer Set: %v", err)
	}
	if _, hit, _ := discogs.Get(ctx, nameKey); hit {
		t.Error("Deezer write served through the Discogs cache — provider namespaces collided")
	}
}
