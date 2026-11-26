package cache_test

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
)

const humbleNameHash = "432522d092e990b60aed5164042056aa"

func TestRedisDeezerEnrichmentCache_KeepsTheNameKeyedCacheContract(t *testing.T) {
	memory.NameKeyedCacheContract(t, func(t *testing.T) memory.NameKeyedCacheUnderTest[domain.DeezerEnrichment] {
		_, client := newRedis(t)
		return cache.NewRedisDeezerEnrichmentCache(client)
	}, domain.DeezerEnrichment{BPM: 172, Explicit: true}, domain.DeezerEnrichment{BPM: 90},
		func(a, b domain.DeezerEnrichment) bool { return a.BPM == b.BPM && a.Explicit == b.Explicit })
}

func TestRedisDeezerLyricsCache_KeepsTheNameKeyedCacheContract(t *testing.T) {
	memory.NameKeyedCacheContract(t, func(t *testing.T) memory.NameKeyedCacheUnderTest[domain.DeezerLyrics] {
		_, client := newRedis(t)
		return cache.NewRedisDeezerLyricsCache(client)
	}, domain.DeezerLyrics{Plain: "la la", Copyright: "c"}, domain.DeezerLyrics{Plain: "other"},
		func(a, b domain.DeezerLyrics) bool { return a.Plain == b.Plain && a.Copyright == b.Copyright })
}

func TestRedisNameKeyedCaches_StoreUnderTheirOwnPrefixesAndTTLs(t *testing.T) {
	tests := []struct {
		name        string
		set         func(ctx context.Context, c *redisCaches) error
		setNegative func(ctx context.Context, c *redisCaches) error
		posKey      string
		posTTL      time.Duration
		negKey      string
		negTTL      time.Duration
	}{
		{
			name: "deezer enrichment",
			set: func(ctx context.Context, c *redisCaches) error {
				return c.enrichment.Set(ctx, "humble kendrick lamar", domain.DeezerEnrichment{BPM: 1})
			},
			setNegative: func(ctx context.Context, c *redisCaches) error {
				return c.enrichment.SetNegative(ctx, "humble kendrick lamar")
			},
			posKey: "discovery:dzenrich:v2:" + humbleNameHash,
			posTTL: 30 * 24 * time.Hour,
			negKey: "discovery:dzenrich:neg:v2:" + humbleNameHash,
			negTTL: 24 * time.Hour,
		},
		{
			name: "deezer lyrics",
			set: func(ctx context.Context, c *redisCaches) error {
				return c.lyrics.Set(ctx, "humble kendrick lamar", domain.DeezerLyrics{Plain: "x"})
			},
			setNegative: func(ctx context.Context, c *redisCaches) error {
				return c.lyrics.SetNegative(ctx, "humble kendrick lamar")
			},
			posKey: "discovery:dzlyrics:v2:" + humbleNameHash,
			posTTL: 90 * 24 * time.Hour,
			negKey: "discovery:dzlyrics:neg:v2:" + humbleNameHash,
			negTTL: 24 * time.Hour,
		},
		{
			name: "generic",
			set:  func(ctx context.Context, c *redisCaches) error { return c.generic.Set(ctx, "humble kendrick lamar", 3) },
			setNegative: func(ctx context.Context, c *redisCaches) error {
				return c.generic.SetNegative(ctx, "humble kendrick lamar")
			},
			posKey: "pos:" + humbleNameHash,
			posTTL: time.Hour,
			negKey: "neg:" + humbleNameHash,
			negTTL: time.Minute,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, caches := newRedisCaches(t)
			ctx := context.Background()
			if err := tt.set(ctx, caches); err != nil {
				t.Fatalf("Set: %v", err)
			}
			if err := tt.setNegative(ctx, caches); err != nil {
				t.Fatalf("SetNegative: %v", err)
			}
			assertStored(t, server, tt.posKey, tt.posTTL)
			assertStored(t, server, tt.negKey, tt.negTTL)
			if got, _ := server.Get(tt.negKey); got != "1" {
				t.Errorf("negative marker = %q, want 1", got)
			}
			if n := len(storedKeys(server)); n != 2 {
				t.Errorf("stored %d keys, want 2: %v", n, storedKeys(server))
			}
		})
	}
}

type redisCaches struct {
	enrichment *cache.RedisNameKeyedCache[domain.DeezerEnrichment]
	lyrics     *cache.RedisNameKeyedCache[domain.DeezerLyrics]
	generic    *cache.RedisNameKeyedCache[int]
}

func newRedisCaches(t *testing.T) (*miniredis.Miniredis, *redisCaches) {
	t.Helper()
	server, client := newRedis(t)
	return server, &redisCaches{
		enrichment: cache.NewRedisDeezerEnrichmentCache(client),
		lyrics:     cache.NewRedisDeezerLyricsCache(client),
		generic:    cache.NewRedisNameKeyedCache(client, "pos:", "neg:", time.Hour, time.Minute, func() int { return 7 }),
	}
}

func TestRedisNameKeyedCache_PositiveRoundTrip(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisDeezerEnrichmentCache(client)
	ctx := context.Background()
	nameKey := "qa-nk|name"

	in := domain.DeezerEnrichment{
		BPM: 92, Gain: -7.1, Explicit: true,
		Label: "Top Dawg", Genres: []string{"Rap/Hip Hop"},
		UPC: "00602547311009", RecordType: "album",
	}
	if err := c.Set(ctx, nameKey, in); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, hit, err := c.Get(ctx, nameKey)
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

	if neg, _ := c.GetNegative(ctx, nameKey); neg {
		t.Error("positive entry reported negative")
	}
	if _, hit, _ := c.Get(ctx, nameKey+"-other"); hit {
		t.Error("different name key hit, want miss")
	}
}

func TestRedisNameKeyedCache_AMissReturnsTheEmptyValue(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisNameKeyedCache(client, "pos:", "neg:", time.Hour, time.Minute, func() int { return 7 })

	if v, hit, err := c.Get(context.Background(), "never-set"); hit || err != nil || v != 7 {
		t.Errorf("Get = (%v,%v,%v), want (empty()=7, miss, nil)", v, hit, err)
	}
}

func TestRedisNameKeyedCache_NegativePath(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisDeezerLyricsCache(client)
	ctx := context.Background()
	nameKey := "qa-nkneg|name"

	if neg, err := c.GetNegative(ctx, nameKey); neg || err != nil {
		t.Fatalf("GetNegative before Set = (%v,%v), want (false,nil)", neg, err)
	}
	if err := c.SetNegative(ctx, nameKey); err != nil {
		t.Fatalf("SetNegative: %v", err)
	}
	if neg, err := c.GetNegative(ctx, nameKey); !neg || err != nil {
		t.Errorf("GetNegative = (%v,%v), want (true,nil)", neg, err)
	}
	if _, hit, _ := c.Get(ctx, nameKey); hit {
		t.Error("negative marker readable as a positive entry")
	}
}

func TestRedisNameKeyedCache_CrossProviderIsolation(t *testing.T) {
	_, client := newRedis(t)
	deezer := cache.NewRedisDeezerEnrichmentCache(client)
	lyrics := cache.NewRedisDeezerLyricsCache(client)
	ctx := context.Background()

	if err := deezer.Set(ctx, "qa-nkiso", domain.DeezerEnrichment{Label: "only deezer"}); err != nil {
		t.Fatalf("deezer Set: %v", err)
	}
	if _, hit, _ := lyrics.Get(ctx, "qa-nkiso"); hit {
		t.Error("Deezer enrichment write served through the lyrics cache, namespaces collided")
	}
}

func TestRedisNameKeyedCache_NilClient_NoOps(t *testing.T) {
	c := cache.NewRedisDeezerEnrichmentCache(nil)
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
	g := cache.NewRedisNameKeyedCache(nil, "pos:", "neg:", time.Hour, time.Minute, func() int { return 7 })
	if v, hit, err := g.Get(ctx, "x"); hit || err != nil || v != 7 {
		t.Errorf("generic nil-client Get = (%v,%v,%v), want (empty()=7, miss)", v, hit, err)
	}
}
