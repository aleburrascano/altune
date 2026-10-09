package discoverytest_test

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"testing"
)

func TestLastFmEnricher_ZeroValueReturnsEmptyEnrichmentAndCountsLookup(t *testing.T) {
	f := &discoverytest.LastFmEnricher{}

	got, err := f.Lookup(context.Background(), domain.ResultKindTrack, "a", "t")

	if err != nil || !got.IsZero() || f.Lookups != 1 {
		t.Fatalf("got (%v, %v) after %d lookups, want empty, nil, 1", got, err, f.Lookups)
	}
}

func TestLastFmEnricher_ReturnsCannedEnrichment(t *testing.T) {
	f := &discoverytest.LastFmEnricher{Enrichment: domain.LastFmEnrichment{Listeners: 7}}

	got, err := f.Lookup(context.Background(), domain.ResultKindTrack, "a", "t")

	if err != nil || got.Listeners != 7 {
		t.Fatalf("got (%v, %v), want the canned enrichment", got, err)
	}
}

func TestLastFmEnricher_LastArgumentsHoldTheSecondCall(t *testing.T) {
	f := &discoverytest.LastFmEnricher{}

	_, _ = f.Lookup(context.Background(), domain.ResultKindTrack, "first", "one")
	_, _ = f.Lookup(context.Background(), domain.ResultKindArtist, "second", "")

	if f.LastKind != domain.ResultKindArtist || f.LastArtist != "second" || f.LastTitle != "" || f.Lookups != 2 {
		t.Fatalf("last = (%v, %q, %q) after %d lookups, want the second call", f.LastKind, f.LastArtist, f.LastTitle, f.Lookups)
	}
}

func TestLyricsProvider_EmptyLyricsIsNilErrorNotAnError(t *testing.T) {
	f := &discoverytest.LyricsProvider{Lyrics: domain.EmptyDeezerLyrics()}

	got, err := f.Lookup(context.Background(), "1")

	if err != nil || !got.IsZero() {
		t.Fatalf("got (%v, %v), want empty lyrics and nil error", got, err)
	}
}

func TestLyricsProvider_LookupErrorReturnsEmptyLyrics(t *testing.T) {
	boom := errors.New("boom")
	f := &discoverytest.LyricsProvider{Lyrics: domain.DeezerLyrics{Plain: "x"}, LookupErr: boom}

	got, err := f.Lookup(context.Background(), "1")

	if !errors.Is(err, boom) || !got.IsZero() {
		t.Fatalf("got (%v, %v), want empty lyrics and boom", got, err)
	}
}

func TestDeezerEnricher_ResolveRecordsArgumentsAndReturnsResolvedID(t *testing.T) {
	f := &discoverytest.DeezerEnricher{ResolvedID: "42"}

	id, err := f.ResolveID(context.Background(), domain.ResultKindTrack, "a", "t")

	if err != nil || id != "42" || f.Resolves != 1 || f.LastArtist != "a" || f.LastTitle != "t" {
		t.Fatalf("got (%q, %v), resolves=%d last=(%q,%q)", id, err, f.Resolves, f.LastArtist, f.LastTitle)
	}
}

func TestDeezerEnricher_ResolveErrorReturnsEmptyID(t *testing.T) {
	boom := errors.New("boom")
	f := &discoverytest.DeezerEnricher{ResolvedID: "42", ResolveErr: boom}

	id, err := f.ResolveID(context.Background(), domain.ResultKindTrack, "a", "t")

	if id != "" || !errors.Is(err, boom) {
		t.Fatalf("got (%q, %v), want (\"\", boom)", id, err)
	}
}
