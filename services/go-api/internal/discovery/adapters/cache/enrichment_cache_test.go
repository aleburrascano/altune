package cache

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
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
