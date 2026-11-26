package cache_test

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/domain"
	"context"
	"reflect"
	"testing"
	"time"
)

const (
	damnKendrickAlbumNegKey    = "discovery:mbenrich:neg:v2:album:0e3e1df5658cc6efe6e47d616ffdb88b"
	kendrickLamarArtistMBIDKey = "discovery:mbid:v2:artist:43138de860cfdd175024a5ccc033d448"
)

func TestRedisEnrichmentCache_NilClientNoOp(t *testing.T) {
	c := cache.NewRedisEnrichmentCache(nil)
	ctx := context.Background()

	if _, found, err := c.Get(ctx, domain.ResultKindAlbum, "mbid"); found || err != nil {
		t.Errorf("nil-client Get must miss with no error, got found=%v err=%v", found, err)
	}
	if err := c.Set(ctx, domain.ResultKindAlbum, "mbid", domain.EmptyEnrichment()); err != nil {
		t.Errorf("nil-client Set must no-op, got %v", err)
	}
	if neg, err := c.GetNegative(ctx, domain.ResultKindAlbum, "name"); neg || err != nil {
		t.Errorf("nil-client GetNegative must be false/no-error, got %v/%v", neg, err)
	}
	if err := c.SetNegative(ctx, domain.ResultKindAlbum, "name"); err != nil {
		t.Errorf("nil-client SetNegative must no-op, got %v", err)
	}
	if m, ok := c.LookupMBID(ctx, domain.ResultKindAlbum, "name"); ok || m != "" {
		t.Errorf("nil-client LookupMBID must report (\"\",false), got %q/%v", m, ok)
	}
	if err := c.RememberMBID(ctx, domain.ResultKindAlbum, "name", "mbid"); err != nil {
		t.Errorf("nil-client RememberMBID must no-op, got %v", err)
	}
}

func TestRedisEnrichmentCache_StoresEachFamilyUnderItsKeyAndTTL(t *testing.T) {
	server, client := newRedis(t)
	c := cache.NewRedisEnrichmentCache(client)
	ctx := context.Background()

	if err := c.Set(ctx, domain.ResultKindAlbum, "abc", domain.MusicBrainzEnrichment{MBID: "abc"}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if err := c.SetNegative(ctx, domain.ResultKindAlbum, "damn kendrick"); err != nil {
		t.Fatalf("SetNegative: %v", err)
	}
	if err := c.RememberMBID(ctx, domain.ResultKindArtist, "kendrick lamar", "mbid-artist-1"); err != nil {
		t.Fatalf("RememberMBID: %v", err)
	}

	assertStored(t, server, "discovery:mbenrich:v1:album:abc", 14*24*time.Hour)
	assertStored(t, server, damnKendrickAlbumNegKey, 24*time.Hour)
	assertStored(t, server, kendrickLamarArtistMBIDKey, 14*24*time.Hour)
	if got, _ := server.Get(kendrickLamarArtistMBIDKey); got != "mbid-artist-1" {
		t.Errorf("stored MBID = %q, want the bare mbid-artist-1", got)
	}
	if n := len(storedKeys(server)); n != 3 {
		t.Errorf("stored %d keys, want 3: %v", n, storedKeys(server))
	}
}

func TestRedisEnrichmentCache_PositiveRoundTrip(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisEnrichmentCache(client)
	ctx := context.Background()

	mbid := "qa-mbid"
	in := domain.MusicBrainzEnrichment{
		MBID:        mbid,
		Genres:      []string{"hip hop", "jazz rap"},
		Year:        2015,
		Rating:      4.3,
		RatingVotes: 120,
		PrimaryType: "Album",
		ExternalIDs: map[domain.ProviderKey]string{"deezer": "111", "discogs": "222"},
		ArtworkURL:  "https://caa/img.jpg",
	}
	if err := c.Set(ctx, domain.ResultKindAlbum, mbid, in); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, hit, err := c.Get(ctx, domain.ResultKindAlbum, mbid)
	if err != nil || !hit {
		t.Fatalf("Get = (hit=%v, err=%v), want hit", hit, err)
	}
	if got.MBID != mbid || got.Year != 2015 || got.Rating != 4.3 || got.RatingVotes != 120 ||
		got.PrimaryType != "Album" || got.ArtworkURL != "https://caa/img.jpg" {
		t.Errorf("enrichment did not round-trip: %+v", got)
	}
	if len(got.Genres) != 2 || got.Genres[0] != "hip hop" || got.Genres[1] != "jazz rap" {
		t.Errorf("genres did not round-trip in order: %v", got.Genres)
	}
	if got.ExternalIDs["deezer"] != "111" || got.ExternalIDs["discogs"] != "222" {
		t.Errorf("external ids = %v, want the cached bridge ids", got.ExternalIDs)
	}

	if _, hit, _ := c.Get(ctx, domain.ResultKindArtist, mbid); hit {
		t.Error("album entry served for artist kind, want kind-namespaced miss")
	}
	if _, hit, _ := c.Get(ctx, domain.ResultKindAlbum, "other-mbid"); hit {
		t.Error("another mbid hit the entry")
	}
}

func TestRedisEnrichmentCache_AMissReturnsTheEmptyEnrichment(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisEnrichmentCache(client)

	got, hit, err := c.Get(context.Background(), domain.ResultKindAlbum, "never-set")
	if hit || err != nil {
		t.Fatalf("Get = (hit=%v, err=%v), want a clean miss", hit, err)
	}
	if want := domain.EmptyEnrichment(); !reflect.DeepEqual(got, want) {
		t.Errorf("miss returned %+v, want the empty enrichment %+v", got, want)
	}
}

func TestRedisEnrichmentCache_NegativePath(t *testing.T) {
	_, client := newRedis(t)
	c := cache.NewRedisEnrichmentCache(client)
	ctx := context.Background()
	nameKey := "qa-neg|name"

	if neg, err := c.GetNegative(ctx, domain.ResultKindAlbum, nameKey); neg || err != nil {
		t.Fatalf("GetNegative before Set = (%v,%v), want (false,nil)", neg, err)
	}
	if err := c.SetNegative(ctx, domain.ResultKindAlbum, nameKey); err != nil {
		t.Fatalf("SetNegative: %v", err)
	}
	if neg, err := c.GetNegative(ctx, domain.ResultKindAlbum, nameKey); !neg || err != nil {
		t.Errorf("GetNegative after Set = (%v,%v), want (true,nil)", neg, err)
	}
	if neg, _ := c.GetNegative(ctx, domain.ResultKindArtist, nameKey); neg {
		t.Error("album negative leaked into artist kind")
	}
	if _, hit, _ := c.Get(ctx, domain.ResultKindAlbum, nameKey); hit {
		t.Error("negative marker readable through the positive path")
	}
}

func TestRedisEnrichmentCache_SetNegativeReturnsTheRedisError(t *testing.T) {
	c := cache.NewRedisEnrichmentCache(unreachableRedisClient(t))
	if err := c.SetNegative(context.Background(), domain.ResultKindAlbum, "name"); err == nil {
		t.Error("SetNegative on an unreachable Redis returned nil, want the error")
	}
	if err := c.RememberMBID(context.Background(), domain.ResultKindAlbum, "name", "mbid"); err == nil {
		t.Error("RememberMBID on an unreachable Redis returned nil, want the error")
	}
}

func TestRedisEnrichmentCache_MBIDIndexRoundTrip(t *testing.T) {
	server, client := newRedis(t)
	c := cache.NewRedisEnrichmentCache(client)
	ctx := context.Background()
	nameKey := "kendrick lamar"

	if _, ok := c.LookupMBID(ctx, domain.ResultKindArtist, nameKey); ok {
		t.Fatal("LookupMBID before Remember, want miss")
	}
	if err := c.RememberMBID(ctx, domain.ResultKindArtist, nameKey, "mbid-artist-1"); err != nil {
		t.Fatalf("RememberMBID: %v", err)
	}
	got, ok := c.LookupMBID(ctx, domain.ResultKindArtist, nameKey)
	if !ok || got != "mbid-artist-1" {
		t.Errorf("LookupMBID = (%q,%v), want (mbid-artist-1,true)", got, ok)
	}
	if _, ok := c.LookupMBID(ctx, domain.ResultKindAlbum, nameKey); ok {
		t.Error("artist name→MBID memo served for album kind")
	}
	if err := c.RememberMBID(ctx, domain.ResultKindArtist, "", "mbid-x"); err != nil {
		t.Errorf("RememberMBID empty name: %v, want nil no-op", err)
	}
	if err := c.RememberMBID(ctx, domain.ResultKindArtist, nameKey, ""); err != nil {
		t.Errorf("RememberMBID empty mbid: %v, want nil no-op", err)
	}
	if got, _ := c.LookupMBID(ctx, domain.ResultKindArtist, nameKey); got != "mbid-artist-1" {
		t.Errorf("empty-mbid Remember clobbered the memo: %q", got)
	}
	if n := len(storedKeys(server)); n != 1 {
		t.Errorf("stored %d keys, want 1: empty names and mbids are not remembered", n)
	}
	if _, ok := c.LookupMBID(ctx, domain.ResultKindArtist, ""); ok {
		t.Error("LookupMBID with empty name, want miss")
	}
}

func TestRedisEnrichmentCache_AnEmptyStoredMBIDIsAMiss(t *testing.T) {
	server, client := newRedis(t)
	c := cache.NewRedisEnrichmentCache(client)
	if err := server.Set(kendrickLamarArtistMBIDKey, ""); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if got, ok := c.LookupMBID(context.Background(), domain.ResultKindArtist, "kendrick lamar"); ok {
		t.Errorf("LookupMBID = (%q,true), want a miss for an empty stored MBID", got)
	}
}
