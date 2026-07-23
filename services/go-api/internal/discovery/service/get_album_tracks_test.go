package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

// fakeAlbumSearcher is a minimal ports.SearchProvider for the fallback path.
type fakeAlbumSearcher struct {
	results []domain.SearchResult
}

func (f *fakeAlbumSearcher) Name() domain.ProviderName { return domain.ProviderDeezer }
func (f *fakeAlbumSearcher) SupportedKinds() map[domain.ResultKind]bool {
	return map[domain.ResultKind]bool{domain.ResultKindAlbum: true}
}
func (f *fakeAlbumSearcher) Search(_ context.Context, _ string, _ map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	return f.results, nil
}

func albumSearchResult(artist, deezerID string) domain.SearchResult {
	return domain.SearchResult{
		Kind: domain.ResultKindAlbum, Title: "Empty Clip", Subtitle: artist,
		Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: deezerID}},
	}
}

// The Deezer search-then-fetch fallback must not return a DIFFERENT artist's
// same-titled album: a bare "Empty Clip" search ranks "Chase Fetti"'s EP first,
// but the requested album is Che's. The artist guard skips the mismatch.
func TestGetAlbumTracks_fallbackSkipsWrongArtist(t *testing.T) {
	var fetchedID string
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			fetchedID = id
			return []domain.SearchResult{{Kind: domain.ResultKindTrack, Title: "Like Lil Mexico",
				Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "cht"}}}}, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{
		albumSearchResult("Chase Fetti", "999"), // wrong artist, ranked first
		albumSearchResult("Che", "111"),         // right artist
	}}
	svc := NewGetAlbumTracksService(
		map[domain.ProviderName]ports.AlbumContentProvider{domain.ProviderDeezer: deezer},
		WithAlbumFallbackSearcher(searcher),
	)

	// SoundCloud isn't in the album-tracks map → falls back to the Deezer search.
	resp, err := svc.Execute(context.Background(), domain.ProviderSoundCloud, "sc-1", "Empty Clip", "Che", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fetchedID != "111" {
		t.Fatalf("fetched deezer album %q, want 111 (Che, not Chase Fetti's 999)", fetchedID)
	}
	if len(resp.Items) != 1 || resp.Items[0].Title != "Like Lil Mexico" {
		t.Fatalf("items = %+v, want Che's tracklist", resp.Items)
	}
}

func TestGetAlbumTracks_fallbackNoArtistMatchReturnsEmpty(t *testing.T) {
	deezer := &fakeAlbumContentProvider{
		getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			t.Fatal("must not fetch a wrong-artist album")
			return nil, nil
		},
	}
	searcher := &fakeAlbumSearcher{results: []domain.SearchResult{albumSearchResult("Chase Fetti", "999")}}
	svc := NewGetAlbumTracksService(
		map[domain.ProviderName]ports.AlbumContentProvider{domain.ProviderDeezer: deezer},
		WithAlbumFallbackSearcher(searcher),
	)

	resp, err := svc.Execute(context.Background(), domain.ProviderSoundCloud, "sc-1", "Empty Clip", "Che", 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Items) != 0 {
		t.Fatalf("items = %d, want 0 (no artist match → empty, not wrong)", len(resp.Items))
	}
}

func TestGetAlbumTracksService_Execute(t *testing.T) {
	sampleTracks := []domain.SearchResult{
		{Kind: domain.ResultKindTrack, Title: "Track 1", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t1"}}},
		{Kind: domain.ResultKindTrack, Title: "Track 2", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t2"}}},
		{Kind: domain.ResultKindTrack, Title: "Track 3", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "t3"}}},
	}

	tests := []struct {
		name          string
		providerName  domain.ProviderName
		externalID    string
		limit         int
		providers     map[domain.ProviderName]ports.AlbumContentProvider
		wantStatus    domain.ProviderStatus
		wantItemCount int
	}{
		{
			name:         "valid provider returns tracks",
			providerName: domain.ProviderDeezer,
			externalID:   "album-123",
			limit:        0,
			providers: map[domain.ProviderName]ports.AlbumContentProvider{
				domain.ProviderDeezer: &fakeAlbumContentProvider{
					getAlbumTracksFn: func(_ context.Context, pn domain.ProviderName, extID string) ([]domain.SearchResult, error) {
						if pn != domain.ProviderDeezer {
							t.Errorf("expected provider deezer, got %s", pn.String())
						}
						if extID != "album-123" {
							t.Errorf("expected externalID album-123, got %s", extID)
						}
						return sampleTracks, nil
					},
				},
			},
			wantStatus:    domain.ProviderStatusOK,
			wantItemCount: 3,
		},
		{
			name:          "unknown provider returns error status",
			providerName:  domain.ProviderSoundCloud,
			externalID:    "album-123",
			limit:         0,
			providers:     map[domain.ProviderName]ports.AlbumContentProvider{},
			wantStatus:    domain.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "provider error returns error status without propagating",
			providerName: domain.ProviderDeezer,
			externalID:   "album-err",
			limit:        0,
			providers: map[domain.ProviderName]ports.AlbumContentProvider{
				domain.ProviderDeezer: &fakeAlbumContentProvider{
					getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return nil, errors.New("upstream timeout")
					},
				},
			},
			wantStatus:    domain.ProviderStatusError,
			wantItemCount: 0,
		},
		{
			name:         "limit truncates results",
			providerName: domain.ProviderDeezer,
			externalID:   "album-123",
			limit:        2,
			providers: map[domain.ProviderName]ports.AlbumContentProvider{
				domain.ProviderDeezer: &fakeAlbumContentProvider{
					getAlbumTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
						return sampleTracks, nil
					},
				},
			},
			wantStatus:    domain.ProviderStatusOK,
			wantItemCount: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewGetAlbumTracksService(tt.providers)

			resp, err := svc.Execute(context.Background(), tt.providerName, tt.externalID, "", "", tt.limit)

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if resp.Status != tt.wantStatus {
				t.Errorf("expected status %s, got %s", tt.wantStatus.String(), resp.Status.String())
			}
			if len(resp.Items) != tt.wantItemCount {
				t.Errorf("expected %d items, got %d", tt.wantItemCount, len(resp.Items))
			}
			if resp.ProviderName != tt.providerName {
				t.Errorf("expected provider name %q, got %q", tt.providerName, resp.ProviderName)
			}
		})
	}
}
