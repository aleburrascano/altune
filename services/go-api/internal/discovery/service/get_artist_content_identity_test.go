package service

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
)

func trackFrom(provider domain.ProviderName, id, title, artist string) domain.SearchResult {
	return domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    title,
		Subtitle: artist,
		Sources:  []domain.SourceRef{{Provider: provider, ExternalID: id}},
	}
}

// The core of the fix for (B): the identity-first path fans out by each provider's
// own id and orders by corroboration, so a track only one provider returns (the
// same-name "Agenda"/"Miley Cyrus" bleed from a wrong id) sinks below the tracks
// multiple providers agree are the artist's.
func TestGetTopTracks_identityFirst_corroboratedBeatsSingleSource(t *testing.T) {
	deezer := &fakeArtistContentProvider{
		getTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				trackFrom(domain.ProviderDeezer, "d-real", "Real Song", "Che"),
				trackFrom(domain.ProviderDeezer, "d-agenda", "Agenda", "Che"),
			}, nil
		},
	}
	itunes := &fakeArtistContentProvider{
		getTopTracksFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				trackFrom(domain.ProviderITunes, "i-real", "Real Song", "Che"),
			}, nil
		},
	}
	store := &fakeIdentityStore{mbid: "mbid-che", xref: map[string]string{"deezer": "d1", "itunes": "i1"}}

	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: deezer,
			domain.ProviderITunes: itunes,
		},
		WithContentIdentityStore(store),
		WithIdentityFirst(),
	)

	resp, err := svc.GetTopTracks(t.Context(), domain.ProviderDeezer, "d1", 10)
	if err != nil {
		t.Fatalf("GetTopTracks error = %v", err)
	}
	if len(resp.Items) != 2 {
		t.Fatalf("items = %d, want 2 merged (Real Song, Agenda)", len(resp.Items))
	}
	if resp.Items[0].Title != "Real Song" {
		t.Errorf("first = %q, want Real Song (2-provider agreement)", resp.Items[0].Title)
	}
	if resp.Items[1].Title != "Agenda" {
		t.Errorf("second = %q, want Agenda (single source, sinks)", resp.Items[1].Title)
	}
	if len(resp.Items[0].Sources) != 2 {
		t.Errorf("Real Song sources = %d, want 2 (merged deezer + itunes)", len(resp.Items[0].Sources))
	}
}

// The core of the fix for (C): albums merge best-of across providers (a cover or
// year from any source fills the gap), and the metadata-less single-source noise
// that used to render as broken cards is dropped. Consensus is left unset here to
// isolate the identity fan-out + merge + hide-bare behaviour.
func TestGetAlbums_identityFirst_bestOfMergeAndHideBare(t *testing.T) {
	deezer := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{
				{Kind: domain.ResultKindAlbum, Title: "Album A", Subtitle: "Che", ImageURL: "cover-a",
					Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "da"}}},
				// Bare: no cover, no year, single source -> should be hidden.
				{Kind: domain.ResultKindAlbum, Title: "Bare Bootleg", Subtitle: "Che",
					Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "db"}}},
			}, nil
		},
	}
	itunes := &fakeArtistContentProvider{
		getAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			// Corroborates Album A and carries the release date Deezer's variant lacked.
			return []domain.SearchResult{
				{Kind: domain.ResultKindAlbum, Title: "Album A", Subtitle: "Che", ReleaseDate: "2020-05-01",
					Sources: []domain.SourceRef{{Provider: domain.ProviderITunes, ExternalID: "ia"}}},
			}, nil
		},
	}
	store := &fakeIdentityStore{mbid: "mbid-che", xref: map[string]string{"deezer": "d1", "itunes": "i1"}}

	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: deezer,
			domain.ProviderITunes: itunes,
		},
		WithContentIdentityStore(store),
		WithIdentityFirst(),
	)

	resp, err := svc.GetAlbums(t.Context(), domain.ProviderDeezer, "d1", "Che", 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if len(resp.Items) != 1 {
		t.Fatalf("items = %d, want 1 (Album A kept, Bare Bootleg hidden): %+v", len(resp.Items), resp.Items)
	}
	a := resp.Items[0]
	if a.Title != "Album A" {
		t.Errorf("title = %q, want Album A", a.Title)
	}
	if a.ImageURL != "cover-a" {
		t.Errorf("ImageURL = %q, want cover-a (best-of from Deezer)", a.ImageURL)
	}
	if a.Year != 2020 {
		t.Errorf("Year = %d, want 2020 (best-of date from iTunes, normalized)", a.Year)
	}
	if len(a.Sources) != 2 {
		t.Errorf("sources = %d, want 2 (merged deezer + itunes)", len(a.Sources))
	}
}

// Identity miss (store has no bridge) degrades to the single-provider path so the
// screen never breaks while identities are still being learned.
func TestGetTopTracks_identityFirst_missFallsBackToSingleProvider(t *testing.T) {
	deezer := &fakeArtistContentProvider{
		getTopTracksFn: func(_ context.Context, _ domain.ProviderName, id string) ([]domain.SearchResult, error) {
			if id != "d1" {
				t.Errorf("fallback used id %q, want the seed d1", id)
			}
			return []domain.SearchResult{trackFrom(domain.ProviderDeezer, "d-real", "Real Song", "Che")}, nil
		},
	}
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{domain.ProviderDeezer: deezer},
		WithContentIdentityStore(&fakeIdentityStore{}), // mbid empty → miss
		WithIdentityFirst(),
	)

	resp, err := svc.GetTopTracks(t.Context(), domain.ProviderDeezer, "d1", 10)
	if err != nil {
		t.Fatalf("GetTopTracks error = %v", err)
	}
	if len(resp.Items) != 1 || resp.Items[0].Title != "Real Song" {
		t.Errorf("items = %+v, want the single-provider fallback result", resp.Items)
	}
}
