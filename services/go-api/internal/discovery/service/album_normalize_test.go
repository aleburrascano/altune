package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestDedupAlbums(t *testing.T) {
	album := func(title string, trackCount int) domain.SearchResult {
		return domain.SearchResult{Kind: domain.ResultKindAlbum, Title: title, TrackCount: trackCount}
	}

	t.Run("collapses normalized-title duplicates keeping higher track_count", func(t *testing.T) {
		in := []domain.SearchResult{
			album("After Hours", 14),
			album("After Hours (Deluxe)", 18),
			album("Starboy", 18),
		}
		out := dedupAlbums(in)
		if len(out) != 2 {
			t.Fatalf("expected 2 albums, got %d: %v", len(out), albumTitles(out))
		}
		if out[0].Title != "After Hours (Deluxe)" {
			t.Errorf("expected deluxe (18 tracks) to win, got %q", out[0].Title)
		}
		if out[1].Title != "Starboy" {
			t.Errorf("expected Starboy retained, got %q", out[1].Title)
		}
	})

	t.Run("keeps first when track_count not higher", func(t *testing.T) {
		in := []domain.SearchResult{album("Dawn FM", 20), album("Dawn FM (Alternate World)", 10)}
		out := dedupAlbums(in)
		if len(out) != 1 || out[0].TrackCount != 20 {
			t.Fatalf("expected single 20-track album, got %v", out)
		}
	})

	t.Run("empty input yields empty output", func(t *testing.T) {
		if out := dedupAlbums(nil); len(out) != 0 {
			t.Fatalf("expected empty, got %v", out)
		}
	})
}

func TestAlbumReleaseSortKey(t *testing.T) {
	cases := []struct {
		name string
		in   domain.SearchResult
		want string
	}{
		{"prefers release_date", domain.SearchResult{ReleaseDate: "2020-03-20", Year: 1999}, "2020-03-20"},
		{"falls back to year", domain.SearchResult{Year: 2015}, "2015"},
		{"empty when neither", domain.SearchResult{}, ""},
		{"empty when year non-positive", domain.SearchResult{Year: 0}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := albumReleaseSortKey(tc.in); got != tc.want {
				t.Errorf("expected %q, got %q", tc.want, got)
			}
		})
	}
}

func TestSortAlbumsByReleaseDateDesc(t *testing.T) {
	album := func(title, releaseDate string, year int) domain.SearchResult {
		return domain.SearchResult{Title: title, ReleaseDate: releaseDate, Year: year}
	}

	t.Run("sorts newest first, undated to the end", func(t *testing.T) {
		in := []domain.SearchResult{
			album("Older", "2016-09-09", 0),
			album("NoDate", "", 0),
			album("Newest", "2022-01-07", 0),
			album("Middle", "2020-03-20", 0),
		}
		sortByReleaseDateDesc(in, albumReleaseSortKey)
		want := []string{"Newest", "Middle", "Older", "NoDate"}
		if got := albumTitles(in); !equalStrings(got, want) {
			t.Errorf("expected %v, got %v", want, got)
		}
	})

	t.Run("stable for equal keys preserves input order", func(t *testing.T) {
		in := []domain.SearchResult{
			album("A", "2020-01-01", 0),
			album("B", "2020-01-01", 0),
			album("C", "2020-01-01", 0),
		}
		sortByReleaseDateDesc(in, albumReleaseSortKey)
		want := []string{"A", "B", "C"}
		if got := albumTitles(in); !equalStrings(got, want) {
			t.Errorf("expected stable %v, got %v", want, got)
		}
	})
}

func TestNormalizeAlbumYears(t *testing.T) {
	in := []domain.SearchResult{
		{Title: "fills from date", ReleaseDate: "2019-05-01"},
		{Title: "keeps existing year", ReleaseDate: "2019-05-01", Year: 1999},
		{Title: "short date untouched", ReleaseDate: "20"},
		{Title: "no date untouched"},
	}
	normalizeAlbumYears(in)
	want := []int{2019, 1999, 0, 0}
	for i, w := range want {
		if in[i].Year != w {
			t.Errorf("%q: expected year %d, got %d", in[i].Title, w, in[i].Year)
		}
	}
}

func TestParseYear(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"2020", 2020},
		{"0", 0},
		{"-5", 0},
		{"", 0},
		{"abcd", 0},
	}
	for _, tc := range cases {
		if got := parseYear(tc.in); got != tc.want {
			t.Errorf("parseYear(%q): expected %d, got %d", tc.in, tc.want, got)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
