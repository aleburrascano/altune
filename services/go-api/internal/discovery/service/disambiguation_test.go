package service

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// --- fillArtwork / disambiguation never-reorder (the display bracket fills
// fields, it must not change the ranked order). Reuses fakeArtworkResolver
// from artwork_fill_test. ---

func TestFillArtwork_FillsArtworkWithoutReordering(t *testing.T) {
	resolver := &fakeArtworkResolver{url: "art://cover.jpg"}
	s := NewService(nil, NewCircuitBreaker(), WithArtworkResolver(resolver))
	in := []domain.SearchResult{
		deezerTrack("Alpha", "A", 30),
		deezerTrack("Bravo", "B", 20),
		deezerTrack("Charlie", "C", 10),
	}
	got := s.fillArtwork(context.Background(), in)

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
	// The engagement-join signature must be computed BEFORE disambiguation fills
	// the artist subtitle: rank keys the behavioral score map pre-fill, and the
	// MB lookup budget makes a post-fill signature flap between searches.
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
	s := NewService(nil, NewCircuitBreaker()) // nil albumValidator → extras-only branch
	in := []domain.SearchResult{
		res(domain.ResultKindArtist, "Nas", "", domain.ProviderDeezer, map[string]any{"disambiguation": "American rapper"}),
		deezerTrack("Some Song", "Nas", 50),
		res(domain.ResultKindArtist, "Genesis", "", domain.ProviderDeezer, map[string]any{"disambiguation": "English rock band"}),
	}
	got := s.applyArtistDisambiguation(context.Background(), in)

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
