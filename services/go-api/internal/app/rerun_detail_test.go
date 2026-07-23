package app

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func albumResult(title string, tracks int, sources ...string) domain.SearchResult {
	refs := make([]domain.SourceRef, len(sources))
	for i, s := range sources {
		pn, _ := domain.ParseProviderName(s)
		refs[i] = domain.SourceRef{Provider: pn, ExternalID: s + "-id"}
	}
	return domain.SearchResult{Kind: domain.ResultKindAlbum, Title: title, TrackCount: tracks, Sources: refs}
}

// The merge below is the one part that must stay faithful to the mobile client's
// dedupAlbumsByTitle — the backend can't import the TS, so these lock the rules.
func TestMergeAlbumsLikeClient_dedupesByTitleKeepsHighestTrackCountUnionsSources(t *testing.T) {
	seeds := []rawSeed{
		{provider: "deezer", status: "ok", items: []domain.SearchResult{albumResult("REST IN BASS", 1, "deezer")}},
		{provider: "itunes", status: "ok", items: []domain.SearchResult{albumResult("Rest in Bass", 12, "applemusic")}},
	}
	got := mergeAlbumsLikeClient(seeds)
	if len(got) != 1 {
		t.Fatalf("want 1 merged album (title dedupe), got %d", len(got))
	}
	if got[0].TrackCount != 12 {
		t.Errorf("want the higher-track-count variant kept (12), got %d", got[0].TrackCount)
	}
	if len(got[0].Sources) != 2 {
		t.Errorf("want both seeds' sources unioned (2), got %d", len(got[0].Sources))
	}
}

func TestMergeAlbumsLikeClient_skipsNonOkSeeds(t *testing.T) {
	seeds := []rawSeed{
		{provider: "deezer", status: "error", items: []domain.SearchResult{albumResult("Ghost", 1, "deezer")}},
		{provider: "soundcloud", status: "ok", items: []domain.SearchResult{albumResult("Real", 2, "soundcloud")}},
	}
	got := mergeAlbumsLikeClient(seeds)
	if len(got) != 1 || got[0].Title != "Real" {
		t.Fatalf("want only the ok seed's album, got %+v", got)
	}
}

func TestMergeAlbumsLikeClient_ordersNewestFirst(t *testing.T) {
	older := albumResult("Older", 0, "deezer")
	older.ReleaseDate = "2019-01-01"
	newer := albumResult("Newer", 0, "deezer")
	newer.ReleaseDate = "2023-06-01"
	undated := albumResult("Undated", 0, "deezer")
	seeds := []rawSeed{{provider: "deezer", status: "ok", items: []domain.SearchResult{older, undated, newer}}}
	got := mergeAlbumsLikeClient(seeds)
	if got[0].Title != "Newer" || got[1].Title != "Older" || got[2].Title != "Undated" {
		t.Errorf("want Newer, Older, Undated; got %s, %s, %s", got[0].Title, got[1].Title, got[2].Title)
	}
}

// Mirrors dedupeTracksByTitle + slice(0,5): first occurrence wins across seeds in
// Deezer-precedence order, deduped by normalized title, capped at five.
func TestMergeTracksLikeClient_dedupesByTitleFirstWinsAndCapsAtFive(t *testing.T) {
	first := []domain.SearchResult{{Title: "A"}, {Title: "B"}}
	second := []domain.SearchResult{{Title: "b"}, {Title: "C"}, {Title: "D"}, {Title: "E"}, {Title: "F"}}
	seeds := []rawSeed{
		{provider: "deezer", status: "ok", items: first},
		{provider: "soundcloud", status: "ok", items: second},
	}
	got := mergeTracksLikeClient(seeds)
	if len(got) != 5 {
		t.Fatalf("want cap 5, got %d", len(got))
	}
	want := []string{"A", "B", "C", "D", "E"} // "b" is a normalized dup of "B" → dropped
	for i, w := range want {
		if got[i].Title != w {
			t.Errorf("pos %d: want %q, got %q", i, w, got[i].Title)
		}
	}
}
