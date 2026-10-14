package main

import "testing"

func group(id, title, primary, date string, secondary ...string) releaseGroup {
	return releaseGroup{ID: id, Title: title, PrimaryType: primary, SecondaryTypes: secondary, FirstReleaseDate: date}
}

func TestChooseRelease_DecisionTable(t *testing.T) {
	cases := []struct {
		name     string
		groups   []releaseGroup
		wantID   string
		wantRule rule
		wantYear int
	}{
		{
			"a plain album beats a compilation of the same song",
			[]releaseGroup{group("hits", "Greatest Hits", "Album", "1999", "Compilation"), group("lp", "Simple Pleasures", "Album", "1988-06-01")},
			"lp", ruleStudioAlbum, 1988,
		},
		{
			"a plain album beats an earlier single",
			[]releaseGroup{group("single", "Careless Whisper", "Single", "1984-07-24"), group("lp", "Make It Big", "Album", "1984-11-01")},
			"lp", ruleStudioAlbum, 1984,
		},
		{
			"the earliest first release date wins over a remaster listed first",
			[]releaseGroup{group("remaster", "Make It Big (2024 Remaster)", "Album", "2024-05-10"), group("lp", "Make It Big", "Album", "1984-11-01")},
			"lp", ruleStudioAlbum, 1984,
		},
		{
			"a live album and a soundtrack never beat a studio album",
			[]releaseGroup{group("live", "Live", "Album", "1980", "Live"), group("ost", "Film", "Album", "1981", "Soundtrack"), group("lp", "Studio", "Album", "1985")},
			"lp", ruleStudioAlbum, 1985,
		},
		{
			"no studio album falls back to the earliest single",
			[]releaseGroup{group("hits", "Hits", "Album", "1990", "Compilation"), group("late", "Song (2024)", "Single", "2024"), group("early", "Song", "Single", "1984-07-24")},
			"early", ruleSingleOrEP, 1984,
		},
		{
			"an EP counts as a single",
			[]releaseGroup{group("ep", "Debut EP", "EP", "2015-03")},
			"ep", ruleSingleOrEP, 2015,
		},
		{
			"a compilation that is the only home stays",
			[]releaseGroup{group("hits", "Hits", "Album", "1990", "Compilation")},
			"hits", ruleOnlyHome, 1990,
		},
		{
			"a live album that is the only home stays",
			[]releaseGroup{group("live", "Live at Wembley", "Album", "1986", "Live")},
			"live", ruleOnlyHome, 1986,
		},
		{
			"ties on the date keep the musicbrainz order",
			[]releaseGroup{group("first", "A", "Album", "1984"), group("second", "B", "Album", "1984")},
			"first", ruleStudioAlbum, 1984,
		},
		{
			"a bare year does not sort before a full date of the same year",
			[]releaseGroup{group("full", "A", "Album", "1975-03-01"), group("year", "B", "Album", "1975")},
			"full", ruleStudioAlbum, 1975,
		},
		{
			"a full date in an earlier year beats a bare later year",
			[]releaseGroup{group("year", "A", "Album", "1976"), group("full", "B", "Album", "1975-12-31")},
			"full", ruleStudioAlbum, 1975,
		},
		{
			"an undated album loses to a dated one",
			[]releaseGroup{group("undated", "A", "Album", ""), group("dated", "B", "Album", "1999")},
			"dated", ruleStudioAlbum, 1999,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := chooseRelease(tc.groups)
			if !ok || got.Group.ID != tc.wantID || got.Rule != tc.wantRule || got.Group.year() != tc.wantYear {
				t.Fatalf("got %+v ok=%v", got, ok)
			}
		})
	}
}

func TestChooseRelease_RefusesWhenOnlyCompilationsAndLiveAlbumsCompete(t *testing.T) {
	cases := map[string][]releaseGroup{
		"no groups": nil,
		"two compilations": {
			group("a", "Hits 1", "Album", "1990", "Compilation"), group("b", "Hits 2", "Album", "1991", "Compilation"),
		},
		"a compilation and a live album": {
			group("a", "Hits", "Album", "1990", "Compilation"), group("b", "Live", "Album", "1991", "Live"),
		},
		"a single with a secondary type is not an original single": {
			group("a", "Song (Live)", "Single", "1990", "Live"), group("b", "Hits", "Album", "1991", "Compilation"),
		},
	}
	for name, groups := range cases {
		t.Run(name, func(t *testing.T) {
			if got, ok := chooseRelease(groups); ok {
				t.Fatalf("picked %+v", got)
			}
		})
	}
}

func TestReleaseGroupYear_ReadsTheLeadingYearOfAPartialDate(t *testing.T) {
	cases := map[string]int{"1984": 1984, "1984-07": 1984, "1984-07-24": 1984, "": 0, "19": 0, "abcd": 0, "19x4-01": 0}
	for date, want := range cases {
		if got := (releaseGroup{FirstReleaseDate: date}).year(); got != want {
			t.Errorf("year(%q) = %d, want %d", date, got, want)
		}
	}
}
