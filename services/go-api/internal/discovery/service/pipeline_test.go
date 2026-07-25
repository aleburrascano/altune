package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

func dzArtist(name string, pop float64) domain.SearchResult {
	return withPop(res(domain.ResultKindArtist, name, "", domain.ProviderDeezer, nil), pop)
}

func norm(raw string) string { return textnorm.NormalizeForMatch(raw) }

func indexOfTitle(results []domain.SearchResult, title string) int {
	for i, r := range results {
		if r.Title == title {
			return i
		}
	}
	return -1
}

func countTitle(results []domain.SearchResult, title string) int {
	n := 0
	for _, r := range results {
		if r.Title == title {
			n++
		}
	}
	return n
}

func inTopN(t *testing.T, got []domain.SearchResult, n int, wantTitle string) {
	t.Helper()
	limit := n
	if len(got) < limit {
		limit = len(got)
	}
	for _, r := range got[:limit] {
		if r.Title == wantTitle {
			return
		}
	}
	t.Fatalf("want %q in top-%d, got order %v", wantTitle, n, titles(got))
}

func TestRankPipeline_GateDominatesPopularity(t *testing.T) {
	itunesOnlyAlbum := withPop(res(domain.ResultKindAlbum, "Mirage", "Some Artist", domain.ProviderITunes, nil), 1_000_000)
	deezerTrack := deezerTrack("Mirage", "Some Artist", 10)

	got := rankPipeline([][]domain.SearchResult{{itunesOnlyAlbum}, {deezerTrack}}, norm("Mirage"))

	if len(got) == 0 {
		t.Fatalf("expected the browseable track to survive, got empty")
	}
	for _, r := range got {
		if r.Kind == domain.ResultKindAlbum {
			t.Fatalf("non-browseable album leaked into results despite the gate: %v", titles(got))
		}
	}
}

func TestRankPipeline_NoKindFavoritism(t *testing.T) {
	albumFirst := withPop(res(domain.ResultKindAlbum, "Echo", "Band", domain.ProviderDeezer, nil), 50)
	trackSecond := withPop(res(domain.ResultKindTrack, "Echo", "Band", domain.ProviderDeezer, nil), 50)

	got := rankPipeline([][]domain.SearchResult{{albumFirst, trackSecond}}, norm("Echo"))

	albumPos, trackPos := -1, -1
	for i, r := range got {
		switch r.Kind {
		case domain.ResultKindAlbum:
			albumPos = i
		case domain.ResultKindTrack:
			trackPos = i
		}
	}
	if albumPos == -1 || trackPos == -1 {
		t.Fatalf("expected both album and track to survive, got %v", titles(got))
	}
	if albumPos > trackPos {
		t.Fatalf("kind favoritism: track was promoted above the album that preceded it in input (album=%d track=%d)", albumPos, trackPos)
	}
}

func TestRankPipeline_DiversityPreservesOrder(t *testing.T) {
	in := []domain.SearchResult{
		deezerTrack("Song A", "Solo", 90),
		deezerTrack("Song B", "Solo", 80),
		deezerTrack("Song C", "Solo", 70),
		deezerTrack("Song D", "Solo", 60),
		deezerTrack("Song E", "Solo", 50),
	}
	got := rankPipeline([][]domain.SearchResult{in}, norm("Solo"))

	order := []string{"Song A", "Song B", "Song C", "Song D", "Song E"}
	prev := -1
	for _, title := range order {
		idx := indexOfTitle(got, title)
		if idx == -1 {
			t.Fatalf("%q dropped by the pipeline: %v", title, titles(got))
		}
		if idx <= prev {
			t.Fatalf("diversity promoted %q above its popularity rank: %v", title, titles(got))
		}
		prev = idx
	}
}

func TestRankPipeline_CollapseKeepsSurvivors(t *testing.T) {
	auroraLow := dzArtist("Aurora", 40)
	auroraHigh := dzArtist("Aurora", 95)
	auroraBorealis := dzArtist("Aurora Borealis", 30)

	got := rankPipeline(
		[][]domain.SearchResult{{auroraLow}, {auroraHigh}, {auroraBorealis}},
		norm("Aurora"),
	)

	if c := countTitle(got, "Aurora"); c != 1 {
		t.Fatalf("expected the two 'Aurora' artists to collapse to 1, got %d: %v", c, titles(got))
	}
	if indexOfTitle(got, "Aurora Borealis") == -1 {
		t.Fatalf("distinct artist 'Aurora Borealis' was wrongly collapsed: %v", titles(got))
	}
	survivor := got[indexOfTitle(got, "Aurora")]
	if survivor.Popularity != 95 {
		t.Fatalf("collapse kept the wrong (less popular) survivor: pop=%v", survivor.Popularity)
	}
}

func TestRankPipeline_CanonicalTopThree(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		groups    [][]domain.SearchResult
		wantTitle string
	}{
		{
			name:  "humble surfaces the kendrick track despite a more popular same-name artist",
			query: "Humble",
			groups: [][]domain.SearchResult{
				{dzArtist("Humble", 90)},
				{deezerTrack("HUMBLE.", "Kendrick Lamar", 40)},
				{deezerAlbum("Humble", "Some Artist", 30)},
			},
			wantTitle: "HUMBLE.",
		},
		{
			name:  "drake the artist surfaces among many drake tracks",
			query: "Drake",
			groups: [][]domain.SearchResult{
				{dzArtist("Drake", 100)},
				{
					deezerTrack("God's Plan", "Drake", 999),
					deezerTrack("Hotline Bling", "Drake", 950),
					deezerTrack("In My Feelings", "Drake", 900),
					deezerTrack("One Dance", "Drake", 880),
				},
			},
			wantTitle: "Drake",
		},
		{
			name:  "kendrick lamar humble lands the specific track",
			query: "Kendrick Lamar Humble",
			groups: [][]domain.SearchResult{
				{dzArtist("Humble", 90)},
				{deezerTrack("HUMBLE.", "Kendrick Lamar", 40)},
				{deezerTrack("DNA.", "Kendrick Lamar", 55)},
			},
			wantTitle: "HUMBLE.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := rankPipeline(tt.groups, norm(tt.query))
			inTopN(t, got, 3, tt.wantTitle)
		})
	}
}
