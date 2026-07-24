package cache

import (
	"context"
	"testing"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"

	goredis "github.com/redis/go-redis/v9"
)

// unreachableRedisClient returns a real client pointed at a dead address, so
// every command fails fast — the "Redis is down" degradation path, distinct
// from the nil-client "Redis not configured" path.
func unreachableRedisClient(t *testing.T) *goredis.Client {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{
		Addr:            "127.0.0.1:1",
		DialTimeout:     200 * time.Millisecond,
		ReadTimeout:     200 * time.Millisecond,
		WriteTimeout:    200 * time.Millisecond,
		MaxRetries:      -1, // fail immediately, no backoff
		PoolTimeout:     200 * time.Millisecond,
		MinRetryBackoff: -1,
		MaxRetryBackoff: -1,
	})
	t.Cleanup(func() { client.Close() })
	return client
}

// --- nil client: Redis not configured — every method must no-op cleanly ------

func TestRedisArtworkCache_NilClient_NoOps(t *testing.T) {
	c := NewRedisArtworkCache(nil)
	ctx := context.Background()

	url, source, hit, err := c.Get(ctx, domain.ResultKindTrack, "t", "s", "")
	if url != "" || source != "" || hit || err != nil {
		t.Errorf("nil-client Get = (%q,%q,%v,%v), want clean miss", url, source, hit, err)
	}
	if err := c.Set(ctx, domain.ResultKindTrack, "t", "s", "", "u", "src", ports.ArtworkConfidenceName); err != nil {
		t.Errorf("nil-client Set must no-op, got %v", err)
	}
}

func TestRedisResultCache_NilClient_NoOps(t *testing.T) {
	c := NewRedisResultCache(nil)
	ctx := context.Background()

	if got, hit := c.Get(ctx, "key"); hit || got != nil {
		t.Errorf("nil-client Get = (%v,%v), want clean miss", got, hit)
	}
	// Set must be a silent no-op (it has no error return to begin with).
	c.Set(ctx, "key", []domain.SearchResult{{Title: "x"}})
	if _, hit := c.Get(ctx, "key"); hit {
		t.Error("nil-client Set cached something, want no-op")
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

func TestRedisIdentityStore_NilClient_DelegatesToInner(t *testing.T) {
	inner := &recordingIdentityStore{
		mbid: "mbid-1", xref: map[string]string{"deezer": "9"}, found: true,
	}
	store := NewRedisIdentityStore(inner, nil)
	ctx := context.Background()

	if err := store.PersistBridges(ctx, domain.ResultKindArtist, "mbid-1", map[string]string{"deezer": "9"}); err != nil {
		t.Fatalf("PersistBridges: %v", err)
	}
	if inner.persistCalls != 1 {
		t.Errorf("durable PersistBridges calls = %d, want 1", inner.persistCalls)
	}

	mbid, xref, ok := store.LookupByProviderID(ctx, domain.ResultKindArtist, "deezer", "9")
	if !ok || mbid != "mbid-1" || xref["deezer"] != "9" {
		t.Errorf("nil-client lookup = (%q,%v,%v), want durable-store value", mbid, xref, ok)
	}
	if inner.lookupCalls != 1 {
		t.Errorf("durable lookup calls = %d, want 1", inner.lookupCalls)
	}
}

// --- unreachable Redis: reads degrade to a miss (or the durable tier) --------

func TestCaches_UnreachableRedis_ReadsDegrade(t *testing.T) {
	client := unreachableRedisClient(t)
	ctx := context.Background()

	t.Run("artwork Get is a clean miss", func(t *testing.T) {
		c := NewRedisArtworkCache(client)
		if _, _, hit, err := c.Get(ctx, domain.ResultKindTrack, "t", "s", ""); hit || err != nil {
			t.Errorf("Get = (hit=%v, err=%v), want clean miss", hit, err)
		}
	})

	t.Run("enrichment Get and GetNegative are clean misses", func(t *testing.T) {
		c := NewRedisEnrichmentCache(client)
		if _, hit, err := c.Get(ctx, domain.ResultKindAlbum, "mbid"); hit || err != nil {
			t.Errorf("Get = (hit=%v, err=%v), want clean miss", hit, err)
		}
		if neg, err := c.GetNegative(ctx, domain.ResultKindAlbum, "name"); neg || err != nil {
			t.Errorf("GetNegative = (%v,%v), want (false,nil)", neg, err)
		}
		if _, ok := c.LookupMBID(ctx, domain.ResultKindAlbum, "name"); ok {
			t.Error("LookupMBID hit on unreachable Redis, want miss")
		}
	})

	t.Run("result cache Get is a clean miss", func(t *testing.T) {
		c := NewRedisResultCache(client)
		if got, hit := c.Get(ctx, "key"); hit || got != nil {
			t.Errorf("Get = (%v,%v), want clean miss", got, hit)
		}
	})

	t.Run("name-keyed Get and GetNegative are clean misses", func(t *testing.T) {
		c := NewRedisLastFmEnrichmentCache(client)
		if _, hit, err := c.Get(ctx, "name"); hit || err != nil {
			t.Errorf("Get = (hit=%v, err=%v), want clean miss", hit, err)
		}
		if neg, err := c.GetNegative(ctx, "name"); neg || err != nil {
			t.Errorf("GetNegative = (%v,%v), want (false,nil)", neg, err)
		}
	})

	t.Run("identity lookup falls through to the durable store", func(t *testing.T) {
		inner := &recordingIdentityStore{
			mbid: "mbid-2", xref: map[string]string{"spotify": "s1"}, found: true,
		}
		store := NewRedisIdentityStore(inner, client)
		mbid, _, ok := store.LookupByProviderID(ctx, domain.ResultKindAlbum, "spotify", "s1")
		if !ok || mbid != "mbid-2" {
			t.Errorf("lookup = (%q,%v), want durable-store fallthrough", mbid, ok)
		}
		if inner.lookupCalls != 1 {
			t.Errorf("durable lookup calls = %d, want 1", inner.lookupCalls)
		}
	})
}

// --- key-namespace isolation across the name-keyed constructors --------------

// Two different providers caching under the same name key must never collide:
// a Deezer entry read back as a Last.fm entry would be silent data corruption.
func TestNameKeyedCacheConstructors_DistinctPrefixes(t *testing.T) {
	prefixes := map[string][2]string{
		"deezer":        {NewRedisDeezerEnrichmentCache(nil).posPrefix, NewRedisDeezerEnrichmentCache(nil).negPrefix},
		"lastfm":        {NewRedisLastFmEnrichmentCache(nil).posPrefix, NewRedisLastFmEnrichmentCache(nil).negPrefix},
		"discogs":       {NewRedisDiscogsEnrichmentCache(nil).posPrefix, NewRedisDiscogsEnrichmentCache(nil).negPrefix},
		"discogsArtist": {NewRedisDiscogsArtistEnrichmentCache(nil).posPrefix, NewRedisDiscogsArtistEnrichmentCache(nil).negPrefix},
		"lyrics":        {NewRedisDeezerLyricsCache(nil).posPrefix, NewRedisDeezerLyricsCache(nil).negPrefix},
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

	// Lyrics are static content: their positive TTL must exceed the default
	// name-keyed TTL, or the "cache lyrics long" intent has silently regressed.
	if got := NewRedisDeezerLyricsCache(nil).posTTL; got <= nameKeyedPositiveTTL {
		t.Errorf("lyrics posTTL = %v, want > default %v", got, nameKeyedPositiveTTL)
	}

	// The exported generic constructor must honor its arguments verbatim.
	g := NewRedisNameKeyedCache(nil, "pos:", "neg:", time.Hour, time.Minute, func() int { return 7 })
	if g.posPrefix != "pos:" || g.negPrefix != "neg:" || g.posTTL != time.Hour || g.negTTL != time.Minute {
		t.Errorf("generic constructor mangled its config: %+v", g)
	}
	if v, hit, err := g.Get(context.Background(), "x"); hit || err != nil || v != 7 {
		t.Errorf("generic nil-client Get = (%v,%v,%v), want (empty()=7, miss)", v, hit, err)
	}
}

// --- shared fake --------------------------------------------------------------

// recordingIdentityStore is a configurable durable-store fake that counts calls
// so the cache tier's read-through/write-through behavior is observable.
type recordingIdentityStore struct {
	mbid  string
	xref  map[string]string
	found bool

	persistCalls    int
	lookupCalls     int
	invalidateCalls int
	invalidateErr   error
}

func (f *recordingIdentityStore) PersistBridges(context.Context, domain.ResultKind, string, map[string]string) error {
	f.persistCalls++
	return nil
}

func (f *recordingIdentityStore) LookupByProviderID(context.Context, domain.ResultKind, string, string) (string, map[string]string, bool) {
	f.lookupCalls++
	return f.mbid, f.xref, f.found
}

func (f *recordingIdentityStore) Invalidate(context.Context, domain.ResultKind, string, string) error {
	f.invalidateCalls++
	return f.invalidateErr
}
