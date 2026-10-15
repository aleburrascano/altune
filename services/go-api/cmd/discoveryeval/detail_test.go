package main

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"strings"
	"testing"
)

func sourced(providers ...domain.ProviderName) domain.SearchResult {
	r := domain.SearchResult{Kind: domain.ResultKindAlbum}
	for _, p := range providers {
		r.Sources = append(r.Sources, domain.SourceRef{Provider: p, ExternalID: "x"})
	}
	return r
}

func TestScoreDetailLive(t *testing.T) {
	row := scoreDetailLive("Nirvana", []domain.SearchResult{
		sourced(domain.ProviderDeezer, domain.ProviderSpotify),
		sourced(domain.ProviderDeezer, domain.ProviderSoundCloud),
		sourced(domain.ProviderSoundCloud),
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
	results := []domain.SearchResult{
		{Kind: domain.ResultKindTrack, Title: "Nirvana", Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer}}},
		{Kind: domain.ResultKindArtist, Title: "NIRVANA", Sources: []domain.SourceRef{{Provider: domain.ProviderSpotify}}},
	}

	card, ok := artistCardNamed(results, "nirvana")

	if !ok || card.Kind != domain.ResultKindArtist {
		t.Fatalf("card = %+v, ok = %v, want the artist card", card, ok)
	}
}

const (
	usNirvana = "5b11f4ce-a62d-471e-81fc-a69a8278c7da"
	ukNirvana = "9282c8b4-ca0b-4c6b-b7e3-4f7762dfc4d6"
)

func TestArtistCardWithMBID_PicksTheCardOfThatArtistNotTheFirstSameNameCard(t *testing.T) {
	results := []domain.SearchResult{
		{Kind: domain.ResultKindArtist, Title: "Nirvana", ArtistMBID: ukNirvana, Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "uk"}}},
		{Kind: domain.ResultKindArtist, Title: "Nirvana", ArtistMBID: usNirvana, Sources: []domain.SourceRef{{Provider: domain.ProviderDeezer, ExternalID: "us"}}},
	}

	card, ok := artistCardWithMBID(results, usNirvana)

	if !ok || card.Sources[0].ExternalID != "us" {
		t.Fatalf("card = %+v, ok = %v, want the US band's card", card, ok)
	}
	if _, found := artistCardWithMBID(results[:1], usNirvana); found {
		t.Error("a same-name card of another artist must not stand in for the pinned one")
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
