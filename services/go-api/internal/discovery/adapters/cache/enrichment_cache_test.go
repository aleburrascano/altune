package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"testing"
)

func TestRedisEnrichmentCache_NilClientNoOp(t *testing.T) {
	c := NewRedisEnrichmentCache(nil)
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
	if ids, ok := c.ExternalIDs(ctx, domain.ResultKindArtist, "mbid"); ok || ids != nil {
		t.Errorf("nil-client ExternalIDs must report (nil,false), got %v/%v", ids, ok)
	}
	if m, ok := c.LookupMBID(ctx, domain.ResultKindAlbum, "name"); ok || m != "" {
		t.Errorf("nil-client LookupMBID must report (\"\",false), got %q/%v", m, ok)
	}
	if err := c.RememberMBID(ctx, domain.ResultKindAlbum, "name", "mbid"); err != nil {
		t.Errorf("nil-client RememberMBID must no-op, got %v", err)
	}
}

func TestEnrichmentCacheKey_Deterministic(t *testing.T) {
	if a, b := enrichmentKey(domain.ResultKindAlbum, "abc"), enrichmentKey(domain.ResultKindAlbum, "abc"); a != b {
		t.Errorf("positive key not deterministic: %q vs %q", a, b)
	}
	if enrichmentKey(domain.ResultKindAlbum, "abc") == enrichmentKey(domain.ResultKindArtist, "abc") {
		t.Error("positive key must namespace by kind")
	}
	if enrichmentKey(domain.ResultKindAlbum, "abc") == enrichmentKey(domain.ResultKindAlbum, "xyz") {
		t.Error("positive key must vary by mbid")
	}

	if a, b := enrichmentNegKey(domain.ResultKindAlbum, "damn kendrick"), enrichmentNegKey(domain.ResultKindAlbum, "damn kendrick"); a != b {
		t.Errorf("negative key not deterministic: %q vs %q", a, b)
	}
	if enrichmentNegKey(domain.ResultKindAlbum, "a") == enrichmentNegKey(domain.ResultKindAlbum, "b") {
		t.Error("negative key must vary by name")
	}
}

func TestRedisEnrichmentCache_PositiveRoundTrip(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisEnrichmentCache(client)
	ctx := context.Background()

	mbid := fmt.Sprintf("qa-mbid-%s", t.Name())
	cleanKeys(t, client, enrichmentKey(domain.ResultKindAlbum, mbid))

	in := domain.MBEnrichment{
		MBID:        mbid,
		Genres:      []string{"hip hop", "jazz rap"},
		Year:        2015,
		Rating:      4.3,
		RatingVotes: 120,
		PrimaryType: "Album",
		ExternalIDs: map[string]string{"deezer": "111", "discogs": "222"},
		ArtworkURL:  "https://caa/img.jpg",
	}
	if err := cache.Set(ctx, domain.ResultKindAlbum, mbid, in); err != nil {
		t.Fatalf("Set: %v", err)
	}

	got, hit, err := cache.Get(ctx, domain.ResultKindAlbum, mbid)
	if err != nil || !hit {
		t.Fatalf("Get = (hit=%v, err=%v), want hit", hit, err)
	}
	if got.MBID != mbid || got.Year != 2015 || got.Rating != 4.3 ||
		got.PrimaryType != "Album" || got.ArtworkURL != "https://caa/img.jpg" {
		t.Errorf("enrichment did not round-trip: %+v", got)
	}
	if len(got.Genres) != 2 || got.Genres[0] != "hip hop" {
		t.Errorf("genres did not round-trip in order: %v", got.Genres)
	}

	if _, hit, _ := cache.Get(ctx, domain.ResultKindArtist, mbid); hit {
		t.Error("album entry served for artist kind, want kind-namespaced miss")
	}

	ids, ok := cache.ExternalIDs(ctx, domain.ResultKindAlbum, mbid)
	if !ok || ids["deezer"] != "111" || ids["discogs"] != "222" {
		t.Errorf("ExternalIDs = (%v,%v), want the cached bridge ids", ids, ok)
	}
}

func TestRedisEnrichmentCache_ExternalIDs_EmptyIsMiss(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisEnrichmentCache(client)
	ctx := context.Background()

	mbid := fmt.Sprintf("qa-mbid-noids-%s", t.Name())
	cleanKeys(t, client, enrichmentKey(domain.ResultKindArtist, mbid))

	if err := cache.Set(ctx, domain.ResultKindArtist, mbid, domain.MBEnrichment{MBID: mbid}); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if ids, ok := cache.ExternalIDs(ctx, domain.ResultKindArtist, mbid); ok || ids != nil {
		t.Errorf("ExternalIDs on id-less enrichment = (%v,%v), want (nil,false)", ids, ok)
	}
	if _, ok := cache.ExternalIDs(ctx, domain.ResultKindArtist, "qa-never-cached"); ok {
		t.Error("ExternalIDs on never-cached mbid, want miss")
	}
}

func TestRedisEnrichmentCache_NegativePath(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisEnrichmentCache(client)
	ctx := context.Background()

	nameKey := fmt.Sprintf("qa-neg|%s", t.Name())
	cleanKeys(t, client,
		enrichmentNegKey(domain.ResultKindAlbum, nameKey),
		enrichmentKey(domain.ResultKindAlbum, nameKey),
	)

	if neg, err := cache.GetNegative(ctx, domain.ResultKindAlbum, nameKey); neg || err != nil {
		t.Fatalf("GetNegative before Set = (%v,%v), want (false,nil)", neg, err)
	}
	if err := cache.SetNegative(ctx, domain.ResultKindAlbum, nameKey); err != nil {
		t.Fatalf("SetNegative: %v", err)
	}
	if neg, err := cache.GetNegative(ctx, domain.ResultKindAlbum, nameKey); !neg || err != nil {
		t.Errorf("GetNegative after Set = (%v,%v), want (true,nil)", neg, err)
	}
	if neg, _ := cache.GetNegative(ctx, domain.ResultKindArtist, nameKey); neg {
		t.Error("album negative leaked into artist kind")
	}
	if _, hit, _ := cache.Get(ctx, domain.ResultKindAlbum, nameKey); hit {
		t.Error("negative marker readable through the positive path")
	}
}

func TestRedisEnrichmentCache_MBIDIndexRoundTrip(t *testing.T) {
	client := testRedisClient(t)
	cache := NewRedisEnrichmentCache(client)
	ctx := context.Background()

	nameKey := fmt.Sprintf("qa-mbidindex|%s", t.Name())
	cleanKeys(t, client,
		mbidIndexKey(domain.ResultKindArtist, nameKey),
		mbidIndexKey(domain.ResultKindAlbum, nameKey),
	)

	if _, ok := cache.LookupMBID(ctx, domain.ResultKindArtist, nameKey); ok {
		t.Fatal("LookupMBID before Remember, want miss")
	}
	if err := cache.RememberMBID(ctx, domain.ResultKindArtist, nameKey, "mbid-artist-1"); err != nil {
		t.Fatalf("RememberMBID: %v", err)
	}
	got, ok := cache.LookupMBID(ctx, domain.ResultKindArtist, nameKey)
	if !ok || got != "mbid-artist-1" {
		t.Errorf("LookupMBID = (%q,%v), want (mbid-artist-1,true)", got, ok)
	}
	if _, ok := cache.LookupMBID(ctx, domain.ResultKindAlbum, nameKey); ok {
		t.Error("artist name→MBID memo served for album kind")
	}
	if err := cache.RememberMBID(ctx, domain.ResultKindArtist, "", "mbid-x"); err != nil {
		t.Errorf("RememberMBID empty name: %v, want nil no-op", err)
	}
	if err := cache.RememberMBID(ctx, domain.ResultKindArtist, nameKey, ""); err != nil {
		t.Errorf("RememberMBID empty mbid: %v, want nil no-op", err)
	}
	if got, _ := cache.LookupMBID(ctx, domain.ResultKindArtist, nameKey); got != "mbid-artist-1" {
		t.Errorf("empty-mbid Remember clobbered the memo: %q", got)
	}
	if _, ok := cache.LookupMBID(ctx, domain.ResultKindArtist, ""); ok {
		t.Error("LookupMBID with empty name, want miss")
	}
}
