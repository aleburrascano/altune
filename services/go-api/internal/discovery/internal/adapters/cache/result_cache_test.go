package cache_test

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/adapters/cache"
	"context"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const humbleTrackHash = "96ac93c0efdcfc299ecd35fc50468cbc"

func TestRedisResultCaches_StoreUnderTheirV2KeyAndTTL(t *testing.T) {
	tests := []struct {
		name string
		make func(*goredis.Client, ...cache.Option) *cache.RedisResultCache
		key  string
		ttl  time.Duration
	}{
		{"search results", cache.NewRedisResultCache, "discovery:results:v2:" + humbleTrackHash, 45 * time.Second},
		{"held slates", cache.NewRedisHeldSlateCache, "discovery:heldslate:v2:" + humbleTrackHash, 30 * time.Minute},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server, client := newRedis(t)
			tt.make(client).Set(context.Background(), "humble|track", []domain.SearchResult{{Title: "Humble"}})

			assertStored(t, server, tt.key, tt.ttl)
			if n := len(storedKeys(server)); n != 1 {
				t.Errorf("stored %d keys, want 1: %v", n, storedKeys(server))
			}
		})
	}
}

func TestRedisResultCache_RoundTripAndFreshCopies(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisResultCache(client)
	ctx := context.Background()
	key := "qa-results|key"

	original := []domain.SearchResult{
		{
			Kind:          domain.ResultKindTrack,
			Title:         "Cached Track",
			Subtitle:      "Cached Artist",
			ImageURL:      "https://img/1.jpg",
			RecordingMBID: "mbid-1",
			Xref:          map[domain.ProviderKey]string{"deezer": "42"},
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
	c.Set(ctx, key, original)

	got, hit := c.Get(ctx, key)
	if !hit {
		t.Fatal("expected hit after Set")
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	first := got[0]
	if first.Title != "Cached Track" || first.Subtitle != "Cached Artist" ||
		first.Kind != domain.ResultKindTrack || first.RecordingMBID != "mbid-1" ||
		first.Album != "Cached Album" || first.Duration != 200 || first.ImageURL != "https://img/1.jpg" {
		t.Errorf("first result did not round-trip: %+v", first)
	}
	if first.Xref["deezer"] != "42" {
		t.Errorf("Xref did not round-trip: %v", first.Xref)
	}
	if len(first.Sources) != 1 || first.Sources[0].ExternalID != "42" || first.Sources[0].Provider != domain.ProviderDeezer {
		t.Errorf("Sources did not round-trip: %v", first.Sources)
	}
	if got[1].Kind != domain.ResultKindArtist || got[1].Title != "Cached Artist" {
		t.Errorf("second result did not round-trip: %+v", got[1])
	}

	got[0].Title = "MUTATED"
	got[0].Xref["deezer"] = "corrupted"

	again, hit := c.Get(ctx, key)
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

func TestRedisResultCache_TypedFieldsRoundTrip(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisResultCache(client)
	ctx := context.Background()
	in := []domain.SearchResult{{
		Kind: domain.ResultKindAlbum, Title: "Blue", RecordType: "ep",
		ResolutionTier: domain.StampResolutionTier(domain.EntityResolutionNone),
	}}

	c.Set(ctx, "blue", in)
	out, hit := c.Get(ctx, "blue")

	if !hit || len(out) != 1 {
		t.Fatalf("Get = (%v,%v), want one result", out, hit)
	}
	if out[0].RecordType != "ep" || out[0].ResolutionTier != in[0].ResolutionTier {
		t.Errorf("typed fields did not round-trip: got %+v", out[0])
	}
}

func TestRedisResultCache_RecordTypeIsStoredAsItsBareString(t *testing.T) {
	server, client := newRedis(t)
	cache.NewRedisResultCache(client).Set(context.Background(), "humble|track",
		[]domain.SearchResult{{Kind: domain.ResultKindAlbum, Title: "Blue", RecordType: "ep"}})

	raw, err := server.Get("discovery:results:v2:" + humbleTrackHash)
	if err != nil {
		t.Fatalf("read stored entry: %v", err)
	}
	if !strings.Contains(raw, `"RecordType":"ep"`) {
		t.Errorf("cached record type is no longer the bare string: %s", raw)
	}
}

const legacyV1ResultPayload = `[{"Kind":2,"Title":"Blue","Subtitle":"Band","ImageURL":"","ArtworkSource":"","Confidence":2,"Sources":[{"Provider":1,"ExternalID":"1","URL":""},{"Provider":10,"ExternalID":"2","URL":""}],"Popularity":0,"ISRC":"","MBID":"","UPC":"123","Xref":null,"Year":0,"ReleaseDate":"","TrackCount":5,"ProviderRank":0,"FanCount":0,"Album":"","Duration":0,"DeezerAlbumID":"","Signature":"","Extras":{"genre_id":1,"record_type":"ep","resolution_tier":"upc","upc":"123"}}]`

func TestRedisResultCache_ALegacyV1EntryIsNotRead(t *testing.T) {
	server, client := newRedis(t)
	if err := server.Set("discovery:results:v1:"+humbleTrackHash, legacyV1ResultPayload); err != nil {
		t.Fatalf("seed legacy entry: %v", err)
	}

	if got, hit := cache.NewRedisResultCache(client).Get(context.Background(), "humble|track"); hit {
		t.Errorf("Get served the v1 entry, whose results lack RecordType and ResolutionTier: %+v", got)
	}
}

func TestRedisResultCache_KeyIsolationAndMiss(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisResultCache(client)
	ctx := context.Background()

	c.Set(ctx, "qa-results-a", []domain.SearchResult{{Title: "A"}})

	if _, hit := c.Get(ctx, "qa-results-b"); hit {
		t.Error("a different composite key must miss, got hit")
	}
	if _, hit := cache.NewRedisHeldSlateCache(client).Get(ctx, "qa-results-a"); hit {
		t.Error("the held slate cache read a search result entry")
	}
}

func TestRedisResultCache_CorruptValueIsMiss(t *testing.T) {
	server, client := newRedis(t)
	if err := server.Set("discovery:results:v2:"+humbleTrackHash, "{not json["); err != nil {
		t.Fatalf("seed corrupt value: %v", err)
	}
	if got, hit := cache.NewRedisResultCache(client).Get(context.Background(), "humble|track"); hit || got != nil {
		t.Errorf("corrupt value Get = (%v,%v), want clean miss", got, hit)
	}
}

func TestRedisResultCache_NilClient_NoOps(t *testing.T) {
	c := cache.NewRedisResultCache(nil)
	ctx := context.Background()

	if got, hit := c.Get(ctx, "key"); hit || got != nil {
		t.Errorf("nil-client Get = (%v,%v), want clean miss", got, hit)
	}
	c.Set(ctx, "key", []domain.SearchResult{{Title: "x"}})
	if _, hit := c.Get(ctx, "key"); hit {
		t.Error("nil-client Set cached something, want no-op")
	}
}

func TestRedisProviderResultCache_PerProviderAnswersShareOneTTL(t *testing.T) {
	server, client := newRedis(t)
	c := cache.NewRedisProviderResultCache(client, 10*time.Minute)
	ctx := context.Background()

	c.Set(ctx, domain.ProviderDeezer, "humble", []domain.SearchResult{{Kind: domain.ResultKindTrack, Title: "Humble"}})
	c.Set(ctx, domain.ProviderMusicBrainz, "humble", nil)

	got, hit := c.Get(ctx, domain.ProviderDeezer, "humble")
	if !hit || len(got) != 1 || got[0].Title != "Humble" {
		t.Fatalf("deezer answer did not round-trip: hit=%v got=%+v", hit, got)
	}
	if got, hit := c.Get(ctx, domain.ProviderMusicBrainz, "humble"); !hit || len(got) != 0 {
		t.Fatalf("an empty answer must be a hit with no results: hit=%v got=%+v", hit, got)
	}
	if _, hit := c.Get(ctx, domain.ProviderSpotify, "humble"); hit {
		t.Fatal("another provider must not see deezer's answer")
	}

	deezerKey := "discovery:provider:v1:ef8bf687a976a775b2b7201faf4333a1"
	musicbrainzKey := "discovery:provider:v1:e9c0b4c3d9cdea8ea0b90b4eb10dfe5c"
	assertStored(t, server, deezerKey, 10*time.Minute)
	assertStored(t, server, musicbrainzKey, 10*time.Minute)
	if raw, _ := server.Get(musicbrainzKey); raw != "[]" {
		t.Errorf("an empty answer is stored as %q, want []", raw)
	}
}

func TestRedisProviderResultCache_NilClient_NoOps(t *testing.T) {
	c := cache.NewRedisProviderResultCache(nil, time.Minute)
	ctx := context.Background()

	c.Set(ctx, domain.ProviderDeezer, "humble", []domain.SearchResult{{Title: "Humble"}})
	if got, hit := c.Get(ctx, domain.ProviderDeezer, "humble"); hit || got != nil {
		t.Errorf("nil-client Get = (%v,%v), want clean miss", got, hit)
	}
}

func TestRedisProviderResultCache_UnreachableRedisIsAMiss(t *testing.T) {
	c := cache.NewRedisProviderResultCache(unreachableRedisClient(t), time.Minute)
	ctx := context.Background()

	c.Set(ctx, domain.ProviderDeezer, "humble", []domain.SearchResult{{Title: "Humble"}})
	if _, hit := c.Get(ctx, domain.ProviderDeezer, "humble"); hit {
		t.Fatal("an unreachable Redis must read as a miss, never as an empty answer")
	}
}
