package main

import (
	"altune/go-api/internal/discovery/domain"
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
