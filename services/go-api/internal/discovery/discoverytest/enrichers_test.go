package discoverytest_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"errors"
	"testing"
)

func TestLyricsProvider_EmptyLyricsIsNilErrorNotAnError(t *testing.T) {
	f := &discovery.FakeLyricsProvider{Lyrics: discovery.EmptyDeezerLyrics()}

	got, err := f.Lookup(context.Background(), "1")

	if err != nil || !got.IsZero() {
		t.Fatalf("got (%v, %v), want empty lyrics and nil error", got, err)
	}
}

func TestLyricsProvider_LookupErrorReturnsEmptyLyrics(t *testing.T) {
	boom := errors.New("boom")
	f := &discovery.FakeLyricsProvider{Lyrics: discovery.DeezerLyrics{Plain: "x"}, LookupErr: boom}

	got, err := f.Lookup(context.Background(), "1")

	if !errors.Is(err, boom) || !got.IsZero() {
		t.Fatalf("got (%v, %v), want empty lyrics and boom", got, err)
	}
}

func TestDeezerEnricher_ResolveRecordsArgumentsAndReturnsResolvedID(t *testing.T) {
	f := &discovery.FakeDeezerEnricher{ResolvedID: "42"}

	id, err := f.ResolveID(context.Background(), discovery.ResultKindTrack, "a", "t")

	if err != nil || id != "42" || f.Resolves != 1 || f.LastArtist != "a" || f.LastTitle != "t" {
		t.Fatalf("got (%q, %v), resolves=%d last=(%q,%q)", id, err, f.Resolves, f.LastArtist, f.LastTitle)
	}
}

func TestDeezerEnricher_ResolveErrorReturnsEmptyID(t *testing.T) {
	boom := errors.New("boom")
	f := &discovery.FakeDeezerEnricher{ResolvedID: "42", ResolveErr: boom}

	id, err := f.ResolveID(context.Background(), discovery.ResultKindTrack, "a", "t")

	if id != "" || !errors.Is(err, boom) {
		t.Fatalf("got (%q, %v), want (\"\", boom)", id, err)
	}
}
