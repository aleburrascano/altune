package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"context"
	"errors"
	"sync"
	"testing"
)

func TestFillArtwork_FillsArtworkWithoutReordering(t *testing.T) {
	resolver := &fakeArtworkResolver{url: "art://cover.jpg"}
	s := NewService(nil, NewCircuitBreaker(), WithArtworkResolver(resolver))
	in := []domain.SearchResult{
		deezerTrack("Alpha", "A", 30),
		deezerTrack("Bravo", "B", 20),
		deezerTrack("Charlie", "C", 10),
	}
	got := s.artwork.fill(context.Background(), in)

	want := []string{"Alpha", "Bravo", "Charlie"}
	if len(got) != len(want) {
		t.Fatalf("fillArtwork changed length: %v", titles(got))
	}
	for i, title := range want {
		if got[i].Title != title {
			t.Fatalf("fillArtwork reordered results: got %v, want %v", titles(got), want)
		}
		if got[i].ImageURL != "art://cover.jpg" {
			t.Fatalf("fillArtwork did not fill artwork for %q (got %q) — test is not exercising enrichment", title, got[i].ImageURL)
		}
	}
}

func TestSearch_SignatureStampedBeforeDisambiguationFill(t *testing.T) {
	artist := res(domain.ResultKindArtist, "Nas", "", domain.ProviderDeezer, map[string]any{"disambiguation": "American rapper"})
	p := &fakeProvider{name: domain.ProviderDeezer, results: []domain.SearchResult{artist}}
	svc := NewService([]ports.SearchProvider{p}, NewCircuitBreaker())

	out := runSearch(t, svc, "nas")

	if len(out.Results) != 1 {
		t.Fatalf("want 1 result, got %v", titles(out.Results))
	}
	got := out.Results[0]
	if got.Subtitle != "American rapper" {
		t.Fatalf("precondition: disambiguation must have filled the subtitle, got %q", got.Subtitle)
	}
	want := domain.ResultSignature(domain.SearchResult{Kind: domain.ResultKindArtist, Title: "Nas"})
	if got.Signature != want {
		t.Errorf("Signature = %q, want pre-fill %q (post-fill would be %q)",
			got.Signature, want, domain.ResultSignature(got))
	}
}

func TestApplyArtistDisambiguation_FillsSubtitleWithoutReordering(t *testing.T) {
	s := NewService(nil, NewCircuitBreaker())
	in := []domain.SearchResult{
		res(domain.ResultKindArtist, "Nas", "", domain.ProviderDeezer, map[string]any{"disambiguation": "American rapper"}),
		deezerTrack("Some Song", "Nas", 50),
		res(domain.ResultKindArtist, "Genesis", "", domain.ProviderDeezer, map[string]any{"disambiguation": "English rock band"}),
	}
	got := s.disambiguator.apply(context.Background(), in)

	want := []string{"Nas", "Some Song", "Genesis"}
	for i, title := range want {
		if got[i].Title != title {
			t.Fatalf("disambiguation reordered results: got %v, want %v", titles(got), want)
		}
	}
	if got[0].Subtitle != "American rapper" || got[2].Subtitle != "English rock band" {
		t.Fatalf("disambiguation did not fill subtitles: %q / %q", got[0].Subtitle, got[2].Subtitle)
	}
}

type countingIdentityResolver struct {
	mu     sync.Mutex
	calls  []string
	byName map[string]*ports.ArtistIdentity
	err    error
}

func (r *countingIdentityResolver) ResolveArtistIdentity(_ context.Context, name string) (*ports.ArtistIdentity, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, name)
	if r.err != nil {
		return nil, r.err
	}
	return r.byName[name], nil
}

func disambigArtist(name string) domain.SearchResult {
	return res(domain.ResultKindArtist, name, "", domain.ProviderDeezer, nil)
}

func TestApplyArtistDisambiguation_PreResolvedExtrasAreFree(t *testing.T) {
	resolver := &countingIdentityResolver{}
	svc := NewService(nil, NewCircuitBreaker(), WithAlbumValidator(resolver))

	withExtra := disambigArtist("Che")
	withExtra.Extras = map[string]any{"disambiguation": "American rapper"}
	out := svc.disambiguator.apply(context.Background(), []domain.SearchResult{withExtra})

	if out[0].Subtitle != "American rapper" {
		t.Errorf("subtitle = %q, want the pre-resolved extra applied", out[0].Subtitle)
	}
	if len(resolver.calls) != 0 {
		t.Errorf("live lookups = %v, want none (extras are free)", resolver.calls)
	}
}

func TestApplyArtistDisambiguation_LiveLookupFillsSubtitleMBIDAndExtras(t *testing.T) {
	resolver := &countingIdentityResolver{byName: map[string]*ports.ArtistIdentity{
		"Che": {MBID: "mb-che", Disambiguation: "American rapper"},
	}}
	svc := NewService(nil, NewCircuitBreaker(), WithAlbumValidator(resolver))

	out := svc.disambiguator.apply(context.Background(), []domain.SearchResult{disambigArtist("Che")})

	if out[0].Subtitle != "American rapper" || out[0].MBID != "mb-che" {
		t.Errorf("result = subtitle %q mbid %q, want filled from MB", out[0].Subtitle, out[0].MBID)
	}
	if out[0].Extras["disambiguation"] != "American rapper" {
		t.Errorf("extras = %v, want the disambiguation memoized", out[0].Extras)
	}
}

func TestApplyArtistDisambiguation_BudgetCapsLiveLookups(t *testing.T) {
	resolver := &countingIdentityResolver{byName: map[string]*ports.ArtistIdentity{}}
	svc := NewService(nil, NewCircuitBreaker(), WithAlbumValidator(resolver))

	in := []domain.SearchResult{
		disambigArtist("A"), disambigArtist("B"), disambigArtist("A"),
		disambigArtist("C"), disambigArtist("D"), disambigArtist("E"),
	}
	svc.disambiguator.apply(context.Background(), in)

	if len(resolver.calls) != disambigMaxLookups {
		t.Errorf("live lookups = %d (%v), want the %d budget", len(resolver.calls), resolver.calls, disambigMaxLookups)
	}
}

func TestApplyArtistDisambiguation_SkipsNonArtistsAndFilledSubtitles(t *testing.T) {
	resolver := &countingIdentityResolver{byName: map[string]*ports.ArtistIdentity{}}
	svc := NewService(nil, NewCircuitBreaker(), WithAlbumValidator(resolver))

	trk := deezerTrack("Che", "Someone", 50)
	named := disambigArtist("Che")
	named.Subtitle = "already set"
	out := svc.disambiguator.apply(context.Background(), []domain.SearchResult{trk, named})

	if len(resolver.calls) != 0 {
		t.Errorf("live lookups = %v, want none (nothing eligible)", resolver.calls)
	}
	if out[1].Subtitle != "already set" {
		t.Errorf("filled subtitle overwritten: %q", out[1].Subtitle)
	}
}

func TestApplyArtistDisambiguation_ResolverErrorLeavesResultUntouched(t *testing.T) {
	resolver := &countingIdentityResolver{err: errors.New("mb down")}
	svc := NewService(nil, NewCircuitBreaker(), WithAlbumValidator(resolver))

	out := svc.disambiguator.apply(context.Background(), []domain.SearchResult{disambigArtist("Che")})
	if out[0].Subtitle != "" || out[0].MBID != "" {
		t.Errorf("errored lookup must leave the result untouched, got %+v", out[0])
	}
}
