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

func TestSortReleasesByDateDesc_yearOnlyNotForcedOlderThanSameYearFullDate(t *testing.T) {
	yearOnly := albumResult("YearOnly", 0, "discogs")
	yearOnly.Year = 2015
	fullDate := albumResult("FullDate", 0, "deezer")
	fullDate.ReleaseDate = "2015-06-01"

	// Year-only first: a bare year and a same-year full date are contemporaneous,
	// so the stable sort must not demote the year-only item below its same-year peer.
	items := []domain.SearchResult{yearOnly, fullDate}
	sortReleasesByDateDesc(items)
	if items[0].Title != "YearOnly" || items[1].Title != "FullDate" {
		t.Errorf("want YearOnly, FullDate (contemporaneous tie keeps input order); got %s, %s",
			items[0].Title, items[1].Title)
	}

	// Full date first: the tie must also preserve that order, not flip it.
	items = []domain.SearchResult{fullDate, yearOnly}
	sortReleasesByDateDesc(items)
	if items[0].Title != "FullDate" || items[1].Title != "YearOnly" {
		t.Errorf("want FullDate, YearOnly (contemporaneous tie keeps input order); got %s, %s",
			items[0].Title, items[1].Title)
	}
}

func TestSortReleasesByDateDesc_distinctYearsStillOrderNewestFirst(t *testing.T) {
	older := albumResult("OlderYear", 0, "discogs")
	older.Year = 2014
	newer := albumResult("NewerFullDate", 0, "deezer")
	newer.ReleaseDate = "2016-03-01"

	items := []domain.SearchResult{older, newer}
	sortReleasesByDateDesc(items)
	if items[0].Title != "NewerFullDate" || items[1].Title != "OlderYear" {
		t.Errorf("want NewerFullDate, OlderYear; got %s, %s", items[0].Title, items[1].Title)
	}
}

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
	want := []string{"A", "B", "C", "D", "E"}
	for i, w := range want {
		if got[i].Title != w {
			t.Errorf("pos %d: want %q, got %q", i, w, got[i].Title)
		}
	}
}
