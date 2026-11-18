package releasepick_test

import (
	"altune/go-api/internal/shared/releasepick"
	"testing"
)

func group(primary, date string, secondary ...string) releasepick.Group {
	return releasepick.Group{PrimaryType: primary, SecondaryTypes: secondary, Date: date}
}

func TestGroupKind(t *testing.T) {
	cases := []struct {
		name  string
		group releasepick.Group
		want  releasepick.Kind
	}{
		{"plain album", group("Album", "1984"), releasepick.KindStudioAlbum},
		{"mixtape", group("Album", "2015", "Mixtape/Street"), releasepick.KindMixtape},
		{"single", group("Single", "1984"), releasepick.KindSingleOrEP},
		{"ep", group("EP", "2015"), releasepick.KindSingleOrEP},
		{"compilation", group("Album", "1990", "Compilation"), releasepick.KindNone},
		{"live album", group("Album", "1986", "Live"), releasepick.KindNone},
		{"live single", group("Single", "1990", "Live"), releasepick.KindNone},
		{"mixtape that is also live", group("Album", "2015", "Mixtape/Street", "Live"), releasepick.KindNone},
		{"broadcast", group("Broadcast", "2020"), releasepick.KindNone},
		{"no types at all", releasepick.Group{}, releasepick.KindNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.group.Kind(); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestBest_PrefersAlbumThenMixtapeThenSingleAndTheEarliestWithin(t *testing.T) {
	cases := []struct {
		name     string
		groups   []releasepick.Group
		wantDate string
		wantKind releasepick.Kind
	}{
		{"an album beats an earlier single", []releasepick.Group{group("Single", "1984-07-24"), group("Album", "1984-11-01")}, "1984-11-01", releasepick.KindStudioAlbum},
		{"a mixtape beats an earlier single", []releasepick.Group{group("Single", "2014"), group("Album", "2015", "Mixtape/Street")}, "2015", releasepick.KindMixtape},
		{"an album beats an earlier mixtape", []releasepick.Group{group("Album", "2014", "Mixtape/Street"), group("Album", "2016")}, "2016", releasepick.KindStudioAlbum},
		{"the earliest album", []releasepick.Group{group("Album", "2024-05-10"), group("Album", "1984-11-01")}, "1984-11-01", releasepick.KindStudioAlbum},
		{"a full date is not later than a year of the same year", []releasepick.Group{group("Album", "1975"), group("Album", "1975-03-01")}, "1975", releasepick.KindStudioAlbum},
		{"dated beats undated", []releasepick.Group{group("Album", ""), group("Album", "1999")}, "1999", releasepick.KindStudioAlbum},
		{"single when it is all there is", []releasepick.Group{group("Album", "1990", "Compilation"), group("Single", "1984")}, "1984", releasepick.KindSingleOrEP},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, kind, ok := releasepick.Best(tc.groups, func(g releasepick.Group) releasepick.Group { return g })
			if !ok || got.Date != tc.wantDate || kind != tc.wantKind {
				t.Fatalf("got %+v %v ok=%v", got, kind, ok)
			}
		})
	}
}

func TestBest_RefusesWhenNothingIsAnOriginalRelease(t *testing.T) {
	cases := map[string][]releasepick.Group{
		"no groups":       nil,
		"compilation":     {group("Album", "1990", "Compilation")},
		"live and remix":  {group("Album", "1986", "Live"), group("Single", "1990", "Remix")},
		"only broadcasts": {group("Broadcast", "2020", "Live")},
	}
	for name, groups := range cases {
		t.Run(name, func(t *testing.T) {
			if got, kind, ok := releasepick.Best(groups, func(g releasepick.Group) releasepick.Group { return g }); ok {
				t.Fatalf("picked %+v %v", got, kind)
			}
		})
	}
}

func TestCompareDates(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1975", "1976", -1}, {"1976", "1975-12-31", 1}, {"1975", "1975-03", 0}, {"", "", 0}, {"", "1999", 1}, {"1999", "", -1},
	}
	for _, tc := range cases {
		if got := releasepick.CompareDates(tc.a, tc.b); got != tc.want {
			t.Errorf("CompareDates(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}
