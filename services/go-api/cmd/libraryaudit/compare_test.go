package main

import (
	"reflect"
	"strings"
	"testing"
)

func libTrack() libraryTrack {
	return libraryTrack{ID: "t1", Title: "Valentina", Artist: "Daniel Caesar", Album: "Freudian", Year: 2019, AlbumArtist: "Daniel Caesar", DurationSeconds: 236}
}

func catTrack() catalogTrack {
	return catalogTrack{Title: "Valentina", Artist: "Daniel Caesar", Album: "Freudian", DurationSeconds: 236, ReleaseYear: 2019}
}

func categoriesOf(found []finding) []category {
	out := make([]category, 0, len(found))
	for _, f := range found {
		out = append(out, f.Category)
	}
	return out
}

func TestMetadataFindings_AgreeingTrackHasNoFindings(t *testing.T) {
	if got := metadataFindings(libTrack(), catTrack()); len(got) != 0 {
		t.Fatalf("expected none, got %v", got)
	}
}

func TestMetadataFindings_LengthOffByMoreThanTenSecondsSplitsShorterAndLonger(t *testing.T) {
	cases := []struct {
		name     string
		audioSec float64
		want     []category
	}{
		{"shorter", 154, []category{categoryLengthShorter}},
		{"longer", 250, []category{categoryLengthLonger}},
		{"exactly ten under is within tolerance", 226, nil},
		{"exactly ten over is within tolerance", 246, nil},
		{"unknown audio length is not flagged", 0, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lib := libTrack()
			lib.DurationSeconds = tc.audioSec
			got := categoriesOf(metadataFindings(lib, catTrack()))
			if len(got) != len(tc.want) || (len(got) == 1 && got[0] != tc.want[0]) {
				t.Fatalf("got %v want %v", got, tc.want)
			}
		})
	}
}

func TestMetadataFindings_DifferentSongSuppressesOtherChecks(t *testing.T) {
	lib := libTrack()
	lib.Title, lib.Artist, lib.DurationSeconds = "TEC", "Lil Tecca", 100
	cat := catalogTrack{Title: "Ransom", Artist: "Lil Tecca", DurationSeconds: 130, ReleaseYear: 2019}
	got := categoriesOf(metadataFindings(lib, cat))
	if len(got) != 1 || got[0] != categoryWrongSong {
		t.Fatalf("got %v", got)
	}
}

func TestMetadataFindings_SameSongSurvivesVersionSuffixesAndFeatures(t *testing.T) {
	cases := []struct {
		name         string
		libTitle     string
		libArtist    string
		catTitle     string
		catArtist    string
		contributors []string
	}{
		{"remaster suffix", "Valentina", "Daniel Caesar", "Valentina - Remastered", "Daniel Caesar", nil},
		{"feature in parens", "Valentina (feat. X)", "Daniel Caesar", "Valentina", "Daniel Caesar", nil},
		{"casing and punctuation", "VALENTINA!", "daniel caesar", "Valentina", "Daniel Caesar", nil},
		{"artist is a contributor", "Valentina", "Guest", "Valentina", "Daniel Caesar", []string{"Daniel Caesar", "Guest"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lib := libTrack()
			lib.Title, lib.Artist = tc.libTitle, tc.libArtist
			cat := catTrack()
			cat.Title, cat.Artist, cat.Contributors = tc.catTitle, tc.catArtist, tc.contributors
			if !isSameSong(lib, cat) {
				t.Fatal("expected the same song")
			}
		})
	}
}

func TestMetadataFindings_ReportsFieldsTheCatalogCanFill(t *testing.T) {
	lib := libTrack()
	lib.Year, lib.AlbumArtist = 0, "  "
	got := categoriesOf(metadataFindings(lib, catTrack()))
	if len(got) != 2 || got[0] != categoryYearFillable || got[1] != categoryAlbumArtistFillable {
		t.Fatalf("got %v", got)
	}
}

func TestMetadataFindings_NothingToFillWhenCatalogHasNoYear(t *testing.T) {
	lib := libTrack()
	lib.Year = 0
	cat := catTrack()
	cat.ReleaseYear = 0
	if got := metadataFindings(lib, cat); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestIsSameSong_SymbolOnlyArtistMatchesItself(t *testing.T) {
	lib := libTrack()
	lib.Title, lib.Artist = "BACK TO ME", "¥$"
	cat := catalogTrack{Title: "BACK TO ME", Artist: "¥$"}
	if !isSameSong(lib, cat) {
		t.Fatal("expected ¥$ to match ¥$")
	}
}

func TestNamesAgree_SymbolOnlyNamesCompareByFoldedRawText(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"¥$", "¥$", true},
		{"¥$", "¥$ ", true},
		{"＄", "$", true},
		{"¥$", "Kanye West", false},
		{"¥$", "$", false},
		{"", "", false},
	}
	for _, tc := range cases {
		if got := namesAgree(tc.a, tc.b); got != tc.want {
			t.Fatalf("namesAgree(%q, %q) = %v", tc.a, tc.b, got)
		}
	}
}

func TestMetadataFindings_FillsOnlyFromTheTracksOwnAlbum(t *testing.T) {
	cases := []struct {
		name         string
		libraryAlbum string
		catalogAlbum string
		want         []category
		wantFix      bool
	}{
		{"same album", "Stardust", "Stardust", []category{categoryYearFillable, categoryAlbumArtistFillable}, true},
		{"case and diacritics fold", "Étoile (Deluxe)", "etoile (deluxe)", []category{categoryYearFillable, categoryAlbumArtistFillable}, true},
		{"single", "stardust", "petty", []category{categoryYearOtherRelease, categoryAlbumArtistOtherRelease}, false},
		{"compilation", "stardust", "Cafe Chill Vibes", []category{categoryYearOtherRelease, categoryAlbumArtistOtherRelease}, false},
		{"brackets are kept", "Stardust", "Stardust (Deluxe)", []category{categoryYearOtherRelease, categoryAlbumArtistOtherRelease}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lib, cat := libTrack(), catTrack()
			lib.Year, lib.AlbumArtist, lib.Album = 0, "", tc.libraryAlbum
			cat.Album = tc.catalogAlbum
			got := metadataFindings(lib, cat)
			if !reflect.DeepEqual(categoriesOf(got), tc.want) {
				t.Fatalf("got %v", categoriesOf(got))
			}
			for _, f := range got {
				if (f.Fix != nil) != tc.wantFix {
					t.Fatalf("finding %+v: fix present = %v, want %v", f, f.Fix != nil, tc.wantFix)
				}
			}
		})
	}
}

func TestMetadataFindings_OtherReleaseNamesTheCatalogAlbum(t *testing.T) {
	lib, cat := libTrack(), catTrack()
	lib.Year, lib.Album = 0, "stardust"
	cat.Album = "Cafe Chill Vibes"
	got := metadataFindings(lib, cat)
	if len(got) != 1 || !strings.Contains(got[0].Detail, "Cafe Chill Vibes") || !strings.Contains(got[0].Detail, "2019") {
		t.Fatalf("got %+v", got)
	}
}

func TestIsSameSong_AnotherVersionIsNotTheSameSong(t *testing.T) {
	cases := []struct{ libTitle, catTitle string }{
		{"Diamonds", "Diamonds (Remix)"},
		{"Finesse", "Finesse (Remix)"},
		{"Cold Heart (PNAU Remix)", "Cold Heart"},
		{"Take on Me", "Take On Me (2017 Acoustic)"},
		{"Everybody Wants To Rule The World", "Everybody Wants To Rule The World (Live From Franklin, TN)"},
	}
	for _, tc := range cases {
		lib := libTrack()
		lib.Title = tc.libTitle
		cat := catTrack()
		cat.Title, cat.Artist = tc.catTitle, lib.Artist
		if isSameSong(lib, cat) {
			t.Errorf("isSameSong(%q, %q) = true, want false: another version", tc.libTitle, tc.catTitle)
		}
	}
}
