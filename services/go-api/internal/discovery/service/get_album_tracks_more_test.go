package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// erroringAlbumSearcher fails the fallback search itself.
type erroringAlbumSearcher struct{}

func (erroringAlbumSearcher) Name() domain.ProviderName { return domain.ProviderDeezer }
func (erroringAlbumSearcher) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindAlbum: true}
}
func (erroringAlbumSearcher) Search(context.Context, string, map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return nil, errors.New("search down")
}

func albumTracksSvc(deezer ports.AlbumContentProvider, searcher ports.SearchProvider) *GetAlbumTracksService {
	return NewGetAlbumTracksService(
		map[domain.ProviderName]ports.AlbumContentProvider{domain.ProviderDeezer: deezer},
		WithAlbumFallbackSearcher(searcher),
	)
}

func TestGetAlbumTracks_fallbackArtistGuardFoldsDiacritics(t *testing.T) {
	// Normalization strips diacritics on both sides, so "Ché" and "Che" are the
	// same artist to the guard — a provider's accented spelling must not trip it.
	var fetchedID string
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			fetchedID = id
			return []domain.SearchResult{{Kind: domain.ResultKindTrack, Title: "T",
				Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t"}}}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{albumSearchResult("Ché", "42")}}
	svc := albumTracksSvc(deezer, searcher)

	resp, err := svc.Execute(context.Background(), domain.ProviderSoundCloud, "sc-1", "Empty Clip", "Che", 0)
	if err != nil {
		t.Fatal(err)
	}
	if fetchedID != "42" || len(resp.Items) != 1 {
		t.Fatalf("fetched %q items %d, want the accented-subtitle album accepted", fetchedID, len(resp.Items))
	}
}

func TestGetAlbumTracks_fallbackArtistGuardRejectsFeatTaggedSubtitle(t *testing.T) {
	// AIDEV-NOTE: pins a KNOWN FALSE NEGATIVE of the exact-match artist guard
	// (deezerSearchFallback in get_album_tracks.go). The guard compares
	// NormalizeForMatch(subtitle) == NormalizeForMatch(albumArtist) EXACTLY, and
	// normalization deliberately keeps un-bracketed feature tags (the curated
	// feat/ft word list was removed 2026-06-21). So a provider subtitle
	// "Che feat. Lil X" normalizes to "che feat lil x" ≠ "che" and the right
	// album is SKIPPED — the fallback returns empty (safe, but a recall miss).
	// If this test starts failing because items were returned, the false
	// negative was fixed — update this pin, don't suppress it.
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			t.Error("guard unexpectedly accepted the feat-tagged subtitle (known false negative fixed?)")
			return nil, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{albumSearchResult("Che feat. Lil X", "42")}}
	svc := albumTracksSvc(deezer, searcher)

	resp, err := svc.Execute(context.Background(), domain.ProviderSoundCloud, "sc-1", "Empty Clip", "Che", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %d, want 0 (feat-tagged subtitle is skipped by the exact-match guard)", len(resp.Items))
	}
}

func TestGetAlbumTracks_fallbackNoArtistTakesFirstCandidate(t *testing.T) {
	// Known behavior, pinned: with NO album artist the guard is disabled, so a
	// bare-title fallback takes the FIRST sourced album — same-titled albums by
	// other artists can win (the documented risk the guard exists to prevent
	// when an artist IS known).
	var fetchedID string
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			fetchedID = id
			return []domain.SearchResult{{Kind: domain.ResultKindTrack, Title: "T",
				Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t"}}}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{
		{Kind: domain.ResultKindAlbum, Title: "Empty Clip", Subtitle: "Whoever"}, // sourceless → skipped
		albumSearchResult("Chase Fetti", "999"),
	}}
	svc := albumTracksSvc(deezer, searcher)

	resp, err := svc.Execute(context.Background(), domain.ProviderSoundCloud, "sc-1", "Empty Clip", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if fetchedID != "999" || len(resp.Items) != 1 {
		t.Fatalf("fetched %q items %d, want the first SOURCED candidate fetched", fetchedID, len(resp.Items))
	}
}

func TestGetAlbumTracks_fallbackSearcherErrorReturnsEmpty(t *testing.T) {
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			t.Error("no album may be fetched when the fallback search errored")
			return nil, nil
		},
	}
	svc := albumTracksSvc(deezer, erroringAlbumSearcher{})

	resp, err := svc.Execute(context.Background(), domain.ProviderSoundCloud, "sc-1", "Empty Clip", "Che", 0)
	if err != nil {
		t.Fatalf("fallback search failure must degrade, not propagate: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %d, want 0", len(resp.Items))
	}
	if resp.ProviderName != domain.ProviderDeezer {
		t.Errorf("provider = %v, want deezer (the fallback's identity)", resp.ProviderName)
	}
}

func TestGetAlbumTracks_fallbackSkipsCandidateWithNoTracks(t *testing.T) {
	// The first matching album resolves zero tracks → continue to the next
	// candidate instead of returning an empty tracklist.
	fetched := []string{}
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			fetched = append(fetched, id)
			if id == "111" {
				return nil, nil // empty tracklist
			}
			return []domain.SearchResult{{Kind: domain.ResultKindTrack, Title: "T",
				Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t"}}}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{
		albumSearchResult("Che", "111"),
		albumSearchResult("Che", "222"),
	}}
	svc := albumTracksSvc(deezer, searcher)

	resp, err := svc.Execute(context.Background(), domain.ProviderSoundCloud, "sc-1", "Empty Clip", "Che", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(fetched) != 2 || fetched[1] != "222" {
		t.Fatalf("fetched = %v, want the empty candidate skipped and the next tried", fetched)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want the second candidate's tracklist", len(resp.Items))
	}
}

func TestGetAlbumTracks_primaryEmptyWithNoTitleKeepsEmptyOK(t *testing.T) {
	// A supported provider that returns zero tracks, with no album title to fall
	// back on: the empty OK response stands (no fallback possible).
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(context.Context, domain.ProviderName, string) ([]domain.SearchResult, error) {
			return nil, nil
		},
	}
	svc := albumTracksSvc(deezer, &fakeAlbumSearcher{})

	resp, err := svc.Execute(context.Background(), domain.ProviderDeezer, "d-1", "", "", 0)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != domain.ProviderStatusOK || len(resp.Items) != 0 {
		t.Fatalf("resp = %v/%d items, want OK/0", resp.Status, len(resp.Items))
	}
}
