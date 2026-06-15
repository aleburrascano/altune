package cache

import (
	"context"
	"fmt"
	"math"
	"os"
	"testing"
	"time"

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

// --- RedisQueryCache --------------------------------------------------------

func TestRedisQueryCache_SetAndGet_CacheHit(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisQueryCache(client)
	ctx := context.Background()

	// Arrange
	provider := domain.ProviderDeezer
	kindsCSV := "track"
	queryHash := QueryHash(fmt.Sprintf("test-query-cache-hit-%s", t.Name()))
	results := []domain.SearchResult{
		{
			Kind:     domain.ResultKindTrack,
			Title:    "Integration Test Track",
			Subtitle: "Test Artist",
			Sources: []domain.SourceRef{
				{Provider: domain.ProviderDeezer, ExternalID: "12345", URL: "https://deezer.com/track/12345"},
			},
		},
	}

	key := queryCacheKey(provider, kindsCSV, queryHash)
	cleanKeys(t, client, key)

	// Act
	err := cache.Set(ctx, provider, kindsCSV, queryHash, results)
	if err != nil {
		t.Fatalf("Set returned unexpected error: %v", err)
	}

	got, fetchedAt, hit, err := cache.Get(ctx, provider, kindsCSV, queryHash)

	// Assert
	if err != nil {
		t.Fatalf("Get returned unexpected error: %v", err)
	}
	if !hit {
		t.Fatal("expected cache hit, got miss")
	}
	if fetchedAt.IsZero() {
		t.Error("expected non-zero fetchedAt timestamp on cache hit")
	}
	if time.Since(fetchedAt) > 5*time.Second {
		t.Errorf("fetchedAt too old: %v (expected within last 5s)", fetchedAt)
	}
	if len(got) != 1 {
		t.Fatalf("expected 1 result, got %d", len(got))
	}
	if got[0].Title != "Integration Test Track" {
		t.Errorf("expected title %q, got %q", "Integration Test Track", got[0].Title)
	}
	if got[0].Subtitle != "Test Artist" {
		t.Errorf("expected subtitle %q, got %q", "Test Artist", got[0].Subtitle)
	}
}

func TestRedisQueryCache_Get_CacheMiss(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisQueryCache(client)
	ctx := context.Background()

	// Arrange — use a hash that was never set
	provider := domain.ProviderDeezer
	kindsCSV := "track"
	queryHash := QueryHash(fmt.Sprintf("nonexistent-query-%s", t.Name()))

	// Act
	got, _, hit, err := cache.Get(ctx, provider, kindsCSV, queryHash)

	// Assert
	if err != nil {
		t.Fatalf("Get returned unexpected error: %v", err)
	}
	if hit {
		t.Fatal("expected cache miss, got hit")
	}
	if got != nil {
		t.Errorf("expected nil results on cache miss, got %v", got)
	}
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
	err := cache.Set(ctx, kind, title, subtitle, mbid, url)
	if err != nil {
		t.Fatalf("Set returned unexpected error: %v", err)
	}

	got, hit, err := cache.Get(ctx, kind, title, subtitle, mbid)

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
	err := cache.Set(ctx, kind, title, subtitle, mbid, "")
	if err != nil {
		t.Fatalf("Set returned unexpected error: %v", err)
	}

	got, hit, err := cache.Get(ctx, kind, title, subtitle, mbid)

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
	got, hit, err := cache.Get(ctx, kind, title, "nobody", "")

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

// --- RedisFetchSuccessStore -------------------------------------------------

func TestRedisFetchSuccessStore_RecordAndGetRate_AllSuccess(t *testing.T) {
	client := testRedisClient(t)
	store := NewRedisFetchSuccessStore(client)
	ctx := context.Background()

	// Arrange — use a provider and ensure clean state
	provider := domain.ProviderITunes
	key := fetchSuccessKey(provider)
	client.Del(ctx, key) // pre-clean
	cleanKeys(t, client, key)

	// Act — record 5 successes
	for i := 0; i < 5; i++ {
		if err := store.Record(ctx, provider, true); err != nil {
			t.Fatalf("Record(%d): %v", i, err)
		}
	}

	rate, err := store.GetRate(ctx, provider)

	// Assert
	if err != nil {
		t.Fatalf("GetRate returned unexpected error: %v", err)
	}
	if rate != 1.0 {
		t.Errorf("expected success rate 1.0 after all successes, got %f", rate)
	}
}

func TestRedisFetchSuccessStore_RecordAndGetRate_Mixed(t *testing.T) {
	client := testRedisClient(t)
	store := NewRedisFetchSuccessStore(client)
	ctx := context.Background()

	// Arrange — use a provider unlikely to collide
	provider := domain.ProviderSoundCloud
	key := fetchSuccessKey(provider)
	client.Del(ctx, key)
	cleanKeys(t, client, key)

	// Act — record 3 successes and 2 failures (5 total)
	outcomes := []bool{true, true, true, false, false}
	for i, success := range outcomes {
		if err := store.Record(ctx, provider, success); err != nil {
			t.Fatalf("Record(%d): %v", i, err)
		}
	}

	rate, err := store.GetRate(ctx, provider)

	// Assert: 3 out of 5 = 0.6
	if err != nil {
		t.Fatalf("GetRate returned unexpected error: %v", err)
	}
	if math.Abs(rate-0.6) > 0.01 {
		t.Errorf("expected success rate ~0.6, got %f", rate)
	}
}

func TestRedisFetchSuccessStore_GetRate_NoRecords(t *testing.T) {
	client := testRedisClient(t)
	store := NewRedisFetchSuccessStore(client)
	ctx := context.Background()

	// Arrange — provider with no history
	provider := domain.ProviderTheAudioDB
	key := fetchSuccessKey(provider)
	client.Del(ctx, key)
	cleanKeys(t, client, key)

	// Act
	rate, err := store.GetRate(ctx, provider)

	// Assert: default rate is 1.0 when no data
	if err != nil {
		t.Fatalf("GetRate returned unexpected error: %v", err)
	}
	if rate != 1.0 {
		t.Errorf("expected default rate 1.0 with no records, got %f", rate)
	}
}

func TestRedisFetchSuccessStore_WindowTruncation(t *testing.T) {
	client := testRedisClient(t)
	store := NewRedisFetchSuccessStore(client)
	ctx := context.Background()

	// Arrange
	provider := domain.ProviderLastFM
	key := fetchSuccessKey(provider)
	client.Del(ctx, key)
	cleanKeys(t, client, key)

	// Act — record 15 entries: first 10 failures, then 5 successes (window=10)
	// The 10 most recent should be the 5 successes + 5 failures from the
	// tail of the failures. But since window is FIFO (LPush + LTrim 0..9),
	// the 10 kept are the most recent: 5 successes + 5 failures.
	for i := 0; i < 10; i++ {
		if err := store.Record(ctx, provider, false); err != nil {
			t.Fatalf("Record failure %d: %v", i, err)
		}
	}
	for i := 0; i < 10; i++ {
		if err := store.Record(ctx, provider, true); err != nil {
			t.Fatalf("Record success %d: %v", i, err)
		}
	}

	rate, err := store.GetRate(ctx, provider)

	// Assert: after 20 entries with window=10, only the last 10 (all success) remain
	if err != nil {
		t.Fatalf("GetRate returned unexpected error: %v", err)
	}
	if rate != 1.0 {
		t.Errorf("expected rate 1.0 after window truncation (last 10 all success), got %f", rate)
	}
}

// --- CachedMbidResolver -----------------------------------------------------

func TestCachedMbidResolver_CachesPositiveResult(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()

	callCount := 0
	innerFunc := func(_ context.Context, _ string) (string, error) {
		callCount++
		return "abc-mbid-123", nil
	}

	url := fmt.Sprintf("https://musicbrainz.org/test/%s", t.Name())
	key := mbidCacheKey(url)
	client.Del(ctx, key)
	cleanKeys(t, client, key)

	resolver := NewCachedMbidResolver(client, innerFunc)

	// Act — first call goes to inner
	mbid1, err := resolver.Resolve(ctx, url)
	if err != nil {
		t.Fatalf("Resolve (1st): %v", err)
	}

	// Act — second call should come from cache
	mbid2, err := resolver.Resolve(ctx, url)
	if err != nil {
		t.Fatalf("Resolve (2nd): %v", err)
	}

	// Assert
	if mbid1 != "abc-mbid-123" {
		t.Errorf("expected mbid %q, got %q", "abc-mbid-123", mbid1)
	}
	if mbid2 != "abc-mbid-123" {
		t.Errorf("expected cached mbid %q, got %q", "abc-mbid-123", mbid2)
	}
	if callCount != 1 {
		t.Errorf("expected inner to be called once (cached on 2nd), got %d calls", callCount)
	}
}

func TestCachedMbidResolver_CachesNegativeResult(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()

	callCount := 0
	innerFunc := func(_ context.Context, _ string) (string, error) {
		callCount++
		return "", nil // no MBID found
	}

	url := fmt.Sprintf("https://musicbrainz.org/negative/%s", t.Name())
	key := mbidCacheKey(url)
	client.Del(ctx, key)
	cleanKeys(t, client, key)

	resolver := NewCachedMbidResolver(client, innerFunc)

	// Act
	mbid1, err := resolver.Resolve(ctx, url)
	if err != nil {
		t.Fatalf("Resolve (1st): %v", err)
	}
	mbid2, err := resolver.Resolve(ctx, url)
	if err != nil {
		t.Fatalf("Resolve (2nd): %v", err)
	}

	// Assert
	if mbid1 != "" {
		t.Errorf("expected empty mbid, got %q", mbid1)
	}
	if mbid2 != "" {
		t.Errorf("expected empty cached mbid, got %q", mbid2)
	}
	if callCount != 1 {
		t.Errorf("expected inner called once (negative cached), got %d", callCount)
	}
}

// --- CachedPopularityResolver -----------------------------------------------

func TestCachedPopularityResolver_CachesPositivePopularity(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()

	callCount := 0
	innerFunc := func(_ context.Context, _, _ string) (float64, error) {
		callCount++
		return 0.85, nil
	}

	title := fmt.Sprintf("Pop Song %s", t.Name())
	artist := "Pop Artist"
	key := popularityCacheKey(title, artist)
	client.Del(ctx, key)
	cleanKeys(t, client, key)

	resolver := NewCachedPopularityResolver(client, innerFunc)

	// Act — first call goes to inner
	pop1, found1, err := resolver.GetPopularity(ctx, title, artist)
	if err != nil {
		t.Fatalf("GetPopularity (1st): %v", err)
	}

	// Act — second call should come from cache
	pop2, found2, err := resolver.GetPopularity(ctx, title, artist)
	if err != nil {
		t.Fatalf("GetPopularity (2nd): %v", err)
	}

	// Assert
	if !found1 {
		t.Error("expected found=true on first call")
	}
	if math.Abs(pop1-0.85) > 0.001 {
		t.Errorf("expected popularity 0.85, got %f", pop1)
	}
	if !found2 {
		t.Error("expected found=true on cached call")
	}
	if math.Abs(pop2-0.85) > 0.001 {
		t.Errorf("expected cached popularity 0.85, got %f", pop2)
	}
	if callCount != 1 {
		t.Errorf("expected inner called once (cached on 2nd), got %d", callCount)
	}
}

func TestCachedPopularityResolver_CachesZeroPopularity(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()

	callCount := 0
	innerFunc := func(_ context.Context, _, _ string) (float64, error) {
		callCount++
		return 0, nil // zero popularity
	}

	title := fmt.Sprintf("Unknown Song %s", t.Name())
	artist := "Nobody"
	key := popularityCacheKey(title, artist)
	client.Del(ctx, key)
	cleanKeys(t, client, key)

	resolver := NewCachedPopularityResolver(client, innerFunc)

	// Act
	pop1, _, err := resolver.GetPopularity(ctx, title, artist)
	if err != nil {
		t.Fatalf("GetPopularity (1st): %v", err)
	}
	pop2, _, err := resolver.GetPopularity(ctx, title, artist)
	if err != nil {
		t.Fatalf("GetPopularity (2nd): %v", err)
	}

	// Assert
	if pop1 != 0 {
		t.Errorf("expected popularity 0, got %f", pop1)
	}
	if pop2 != 0 {
		t.Errorf("expected cached popularity 0, got %f", pop2)
	}
	if callCount != 1 {
		t.Errorf("expected inner called once (negative cached), got %d", callCount)
	}
}
