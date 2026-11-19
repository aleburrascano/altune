package main

import (
	"altune/go-api/internal/discovery"
	"context"
	"strings"
	"testing"
)

func sourced(providers ...discovery.ProviderName) discovery.SearchResult {
	r := discovery.SearchResult{Kind: discovery.ResultKindAlbum}
	for _, p := range providers {
		r.Sources = append(r.Sources, discovery.SourceRef{Provider: p, ExternalID: "x"})
	}
	return r
}

func TestScoreDetailLive(t *testing.T) {
	row := scoreDetailLive("Nirvana", []discovery.SearchResult{
		sourced(discovery.ProviderDeezer, discovery.ProviderSpotify),
		sourced(discovery.ProviderDeezer, discovery.ProviderSoundCloud),
		sourced(discovery.ProviderSoundCloud),
	})

	if row.Items != 3 || row.Confirmed != 1 || row.SoundCloudOnly != 1 {
		t.Fatalf("row = %+v, want 3 items, 1 confirmed by two catalogs, 1 on SoundCloud only", row)
	}
}

func TestSummarizeDetailLive(t *testing.T) {
	report := summarizeDetailLive([]detailLiveRow{
		{Items: 3, Confirmed: 3, AlbumsMs: 900},
		{Items: 1, Confirmed: 0, SoundCloudOnly: 1, AlbumsMs: 100},
		{Items: 0, AlbumsMs: 300},
	})

	if report.ConfirmedShare != 0.75 || report.SoundCloudOnly != 1 || report.P50Ms != 300 || report.MaxMs != 900 {
		t.Fatalf("report = %+v, want 75%% confirmed, 1 SoundCloud only, p50 300ms, max 900ms", report)
	}
}

func TestArtistCardNamed(t *testing.T) {
	results := []discovery.SearchResult{
		{Kind: discovery.ResultKindTrack, Title: "Nirvana", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer}}},
		{Kind: discovery.ResultKindArtist, Title: "NIRVANA", Sources: []discovery.SourceRef{{Provider: discovery.ProviderSpotify}}},
	}

	card, ok := artistCardNamed(results, "nirvana")

	if !ok || card.Kind != discovery.ResultKindArtist {
		t.Fatalf("card = %+v, ok = %v, want the artist card", card, ok)
	}
}

const (
	usNirvana = "5b11f4ce-a62d-471e-81fc-a69a8278c7da"
	ukNirvana = "9282c8b4-ca0b-4c6b-b7e3-4f7762dfc4d6"
)

type seedAlbums map[string][]discovery.SearchResult

func (a seedAlbums) GetAlbums(_ context.Context, provider discovery.ProviderName, externalID string, _ map[discovery.ProviderName]string, _ int) (*discovery.ContentFetchResponse, error) {
	return &discovery.ContentFetchResponse{Items: a[provider.String()+":"+externalID]}, nil
}

func TestDetailLiveRows_FindsEachArtistCardByNameAndListsItsAlbums(t *testing.T) {
	searcher := queryMapSearcher{
		"Nirvana": {
			{Kind: discovery.ResultKindTrack, Title: "Nirvana", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "t1"}}},
			{Kind: discovery.ResultKindArtist, Title: "Nirvana", Sources: []discovery.SourceRef{{Provider: discovery.ProviderDeezer, ExternalID: "415"}}},
		},
	}
	albums := seedAlbums{"deezer:415": {sourced(discovery.ProviderDeezer, discovery.ProviderSpotify)}}
	artists := []liveArtist{{mbid: usNirvana, name: "Nirvana"}, {mbid: ukNirvana, name: "Nirvana (UK)"}}

	rows, missing, err := detailLiveRows(context.Background(), searcher, albums, artists)

	if err != nil || len(rows) != 1 || rows[0].Items != 1 || rows[0].Confirmed != 1 {
		t.Fatalf("rows = %+v, err = %v, want Nirvana's one confirmed album", rows, err)
	}
	if len(rows[0].CardIDs) != 1 || rows[0].CardIDs[0] != "deezer:415" {
		t.Errorf("card ids = %v, want the artist card, not the same-titled track", rows[0].CardIDs)
	}
	if len(missing) != 1 || !strings.Contains(missing[0], "Nirvana (UK)") {
		t.Errorf("missing = %v, want the artist no card is named after", missing)
	}
}

func TestParseLiveArtists_RejectsALineWithoutAnArtistMBID(t *testing.T) {
	if _, err := parseLiveArtists("Nirvana"); err == nil {
		t.Fatal("a bare name is not a pinned artist")
	}
	artists, err := parseLiveArtists(usNirvana + " Nirvana\n" + ukNirvana + " Nirvana (UK)")
	if err != nil || len(artists) != 2 || artists[1].name != "Nirvana (UK)" || artists[0].mbid != usNirvana {
		t.Fatalf("artists = %+v, err = %v, want two pinned artists", artists, err)
	}
}

func TestCommittedLiveArtists_AreAllPinned(t *testing.T) {
	artists, err := parseLiveArtists(detailLiveArtists)

	if err != nil || len(artists) < 10 {
		t.Fatalf("artists = %d, err = %v, want the committed list pinned by MBID", len(artists), err)
	}
}

func TestDetailServiceAdapter_AFetchFailureSurfacesInsteadOfAnEmptyList(t *testing.T) {
	adapter := &detailServiceAdapter{}

	items := adapter.Albums(context.Background(), "not-a-provider", "1", "Che")

	if len(items) != 0 {
		t.Errorf("items = %v, want none for a failed fetch", items)
	}
	err := adapter.fetchFailure()
	if err == nil || !strings.Contains(err.Error(), "Che") {
		t.Fatalf("fetchFailure = %v, want an error naming the artist whose fetch failed", err)
	}
	if (&detailServiceAdapter{}).fetchFailure() != nil {
		t.Error("an adapter with no failed fetch must report none")
	}
}
