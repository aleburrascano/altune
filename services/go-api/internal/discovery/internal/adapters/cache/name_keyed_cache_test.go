package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"testing"
	"time"
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
	if _, hit, _ := cache.Get(ctx, nameKey); hit {
		t.Error("negative marker readable as a positive entry")
	}
}

func TestRedisNameKeyedCache_CrossProviderIsolation(t *testing.T) {
	client := testRedisClient(t)
	deezer := NewRedisDeezerEnrichmentCache(client)
	lastfm := NewRedisLastFmEnrichmentCache(client)
	ctx := context.Background()

	nameKey := fmt.Sprintf("qa-nkiso|%s", t.Name())
	cleanKeys(t, client,
		hashKey(deezer.posPrefix, nameKey),
		hashKey(lastfm.posPrefix, nameKey),
	)

	if err := deezer.Set(ctx, nameKey, domain.DeezerEnrichment{Label: "only deezer"}); err != nil {
		t.Fatalf("deezer Set: %v", err)
	}
	if _, hit, _ := lastfm.Get(ctx, nameKey); hit {
		t.Error("Deezer write served through the Last.fm cache — provider namespaces collided")
	}
}

func TestRedisNameKeyedCache_NilClient_NoOps(t *testing.T) {
	c := NewRedisDeezerEnrichmentCache(nil)
	ctx := context.Background()

	if v, hit, err := c.Get(ctx, "name"); hit || err != nil || !v.IsZero() {
		t.Errorf("nil-client Get = (%+v,%v,%v), want empty miss", v, hit, err)
	}
	if err := c.Set(ctx, "name", domain.DeezerEnrichment{BPM: 120}); err != nil {
		t.Errorf("nil-client Set must no-op, got %v", err)
	}
	if neg, err := c.GetNegative(ctx, "name"); neg || err != nil {
		t.Errorf("nil-client GetNegative = (%v,%v), want (false,nil)", neg, err)
	}
	if err := c.SetNegative(ctx, "name"); err != nil {
		t.Errorf("nil-client SetNegative must no-op, got %v", err)
	}
}

func TestNameKeyedCacheConstructors_DistinctPrefixes(t *testing.T) {
	prefixes := map[string][2]string{
		"deezer": {NewRedisDeezerEnrichmentCache(nil).posPrefix, NewRedisDeezerEnrichmentCache(nil).negPrefix},
		"lastfm": {NewRedisLastFmEnrichmentCache(nil).posPrefix, NewRedisLastFmEnrichmentCache(nil).negPrefix},
		"lyrics": {NewRedisDeezerLyricsCache(nil).posPrefix, NewRedisDeezerLyricsCache(nil).negPrefix},
	}
	seen := map[string]string{}
	for name, pair := range prefixes {
		if pair[0] == pair[1] {
			t.Errorf("%s: positive and negative prefixes are identical (%q)", name, pair[0])
		}
		for _, p := range pair {
			if p == "" {
				t.Errorf("%s: empty prefix", name)
			}
			if other, dup := seen[p]; dup {
				t.Errorf("prefix %q shared by %s and %s — cross-provider key collision", p, other, name)
			}
			seen[p] = name
		}
	}

	if got := NewRedisDeezerLyricsCache(nil).posTTL; got <= nameKeyedPositiveTTL {
		t.Errorf("lyrics posTTL = %v, want > default %v", got, nameKeyedPositiveTTL)
	}

	g := NewRedisNameKeyedCache(nil, "pos:", "neg:", time.Hour, time.Minute, func() int { return 7 })
	if g.posPrefix != "pos:" || g.negPrefix != "neg:" || g.posTTL != time.Hour || g.negTTL != time.Minute {
		t.Errorf("generic constructor mangled its config: %+v", g)
	}
	if v, hit, err := g.Get(context.Background(), "x"); hit || err != nil || v != 7 {
		t.Errorf("generic nil-client Get = (%v,%v,%v), want (empty()=7, miss)", v, hit, err)
	}
}
