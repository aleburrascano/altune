package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

type fakeMBAnchor struct {
	titles []string
	err    error
	calls  int
}

func (f *fakeMBAnchor) ReleaseGroupTitles(_ context.Context, _ string) ([]string, error) {
	f.calls++
	return f.titles, f.err
}

func verifyAlbums(titles ...string) []domain.SearchResult {
	out := make([]domain.SearchResult, 0, len(titles))
	for _, t := range titles {
		out = append(out, domain.SearchResult{Kind: domain.ResultKindAlbum, Title: t})
	}
	return out
}

func albumProvider(fn func() ([]domain.SearchResult, error)) *fakeArtistContentProvider {
	return &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return fn()
		},
	}
}

func TestIdentityVerifier_dropsMisbridgedEdge(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"Alpha", "Bravo", "Charlie", "Delta", "Echo", "Foxtrot"}}
	// deezer resolves to the WRONG same-name artist (no catalogue overlap); spotify
	// resolves to the right one (full overlap).
	deezer := albumProvider(func() ([]domain.SearchResult, error) {
		return verifyAlbums("Nope", "Wrong", "Other", "Bogus", "Zzz"), nil
	})
	spotify := albumProvider(func() ([]domain.SearchResult, error) {
		return verifyAlbums("Alpha", "Bravo", "Charlie", "Delta"), nil
	})
	v := NewIdentityVerifier(anchor, map[domain.ProviderName]ports.ArtistContentProvider{
		domain.ProviderDeezer:  deezer,
		domain.ProviderSpotify: spotify,
	})

	in := map[string]string{"deezer": "wrong-id", "spotify": "right-id", "discogs": "123"}
	out := v.VerifyXref(context.Background(), domain.ResultKindArtist, "mbid-1", in)

	if _, ok := out["deezer"]; ok {
		t.Errorf("mis-bridged deezer edge should be dropped, got %v", out)
	}
	if out["spotify"] != "right-id" {
		t.Errorf("overlapping spotify edge should be kept, got %v", out)
	}
	if out["discogs"] != "123" {
		t.Errorf("non-catalogue discogs edge should be untouched, got %v", out)
	}
	// Input must not be mutated (VerifyXref clones).
	if _, ok := in["deezer"]; !ok {
		t.Error("VerifyXref mutated the input xref")
	}
}

func TestIdentityVerifier_failOpenTooFewReleaseGroups(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"A", "B"}} // < mbAnchorMinReleaseGroups
	deezer := albumProvider(func() ([]domain.SearchResult, error) {
		t.Fatal("must not fetch a provider catalogue when there's no anchor to judge against")
		return nil, nil
	})
	v := NewIdentityVerifier(anchor, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer})

	out := v.VerifyXref(context.Background(), domain.ResultKindArtist, "mbid-2", map[string]string{"deezer": "x"})
	if out["deezer"] != "x" {
		t.Errorf("fail-open expected (too few release-groups), got %v", out)
	}
}

func TestIdentityVerifier_fetchErrorKeepsEdge(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"A", "B", "C", "D", "E"}}
	deezer := albumProvider(func() ([]domain.SearchResult, error) {
		return nil, errors.New("deezer down")
	})
	v := NewIdentityVerifier(anchor, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer})

	out := v.VerifyXref(context.Background(), domain.ResultKindArtist, "mbid-3", map[string]string{"deezer": "x"})
	if out["deezer"] != "x" {
		t.Errorf("a fetch error must keep the edge (fail-open), got %v", out)
	}
}

func TestIdentityVerifier_memoSkipsSecondVerification(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"A", "B", "C", "D", "E"}}
	deezer := albumProvider(func() ([]domain.SearchResult, error) {
		return verifyAlbums("A", "B", "C", "D"), nil
	})
	v := NewIdentityVerifier(anchor, map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer})

	in := map[string]string{"deezer": "x"}
	v.VerifyXref(context.Background(), domain.ResultKindArtist, "mbid-4", in)
	v.VerifyXref(context.Background(), domain.ResultKindArtist, "mbid-4", in)
	if anchor.calls != 1 {
		t.Errorf("anchor fetched %d times, want 1 (second call memoized)", anchor.calls)
	}
}

func TestIdentityVerifier_nonArtistKindUntouched(t *testing.T) {
	anchor := &fakeMBAnchor{titles: []string{"A", "B", "C", "D", "E"}}
	v := NewIdentityVerifier(anchor, nil)
	in := map[string]string{"deezer": "x"}
	out := v.VerifyXref(context.Background(), domain.ResultKindAlbum, "mbid-5", in)
	if out["deezer"] != "x" || anchor.calls != 0 {
		t.Errorf("album-kind identity must be untouched (verification is artist-only)")
	}
}
