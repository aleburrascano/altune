package main

import (
	"slices"
	"testing"
)

func makeItBig() releaseGroup {
	g := group("lp", "Make It Big", "Album", "1984-11-01")
	g.Artist = "Wham!"
	return g
}

func TestDerive_TakesYearFromTheChosenReleaseAndDeezerFieldsFromTheCrossCheck(t *testing.T) {
	a := anchor{ISRC: "GBAYE8400001", Title: "Careless Whisper", Artist: "Wham!"}
	match := &deezerMatch{AlbumTitle: "Make It Big", CoverURL: "https://cdn/mib.jpg", Genre: "Pop", TrackPosition: 4, DurationSeconds: 300}
	got := derive(a, makeItBig(), match)
	want := desired{
		Title: "Careless Whisper", Artist: "Wham!", Album: "Make It Big", AlbumArtist: "Wham!", Year: 1984, TrackNumber: 4,
		Genre: "Pop", CatalogDurationSeconds: 300, ArtworkURL: "https://cdn/mib.jpg", ISRC: "GBAYE8400001",
	}
	if got != want {
		t.Fatalf("got %+v", got)
	}
}

func TestDerive_WithoutADeezerAlbumFillsNothingFromDeezer(t *testing.T) {
	got := derive(anchor{Title: "Careless Whisper"}, makeItBig(), nil)
	if got.TrackNumber != 0 || got.Genre != "" || got.CatalogDurationSeconds != 0 || got.ArtworkURL != "" || got.Year != 1984 {
		t.Fatalf("got %+v", got)
	}
}

func fields(found []difference) map[string]difference {
	byField := map[string]difference{}
	for _, d := range found {
		byField[d.Field] = d
	}
	return byField
}

func TestDiff_ABobbyMcFerrinTrackStoredOnACompilationReportsItsAlbumAndOnlyCorrectsAlbumIndependentFields(t *testing.T) {
	stored := libraryTrack{
		Title: "Don't Worry, Be Happy", Artist: "Bobby McFerrin", Album: "Now That's What I Call Music! 14", AlbumArtist: "Various Artists",
		ISRC: "USEM38800146", ArtworkURL: "https://cdn/now14.jpg", Year: 1999, Genre: "Pop", TrackNumber: 12, CatalogDurationSeconds: 240,
	}
	g := group("lp", "Simple Pleasures", "Album", "1988-06-01")
	g.Artist = "Bobby McFerrin"
	want := derive(anchor{ISRC: "USEM38800146", Title: "Don't Worry, Be Happy", Artist: "Bobby McFerrin"}, g,
		&deezerMatch{CoverURL: "https://cdn/sp.jpg", Genre: "Jazz", TrackPosition: 2, DurationSeconds: 292})
	got := fields(diff(stored, want))
	cases := map[string]struct {
		mode changeMode
		old  string
		new  string
	}{
		"album":                    {modeReportOnly, "Now That's What I Call Music! 14", "Simple Pleasures"},
		"year":                     {modeCorrect, "1999", "1988"},
		"album_artist":             {modeCorrect, "Various Artists", "Bobby McFerrin"},
		"track_number":             {modeReportOnly, "12", "2"},
		"genre":                    {modeReportOnly, "Pop", "Jazz"},
		"catalog_duration_seconds": {modeCorrect, "240", "292"},
		"artwork_url":              {modeReportOnly, "https://cdn/now14.jpg", "https://cdn/sp.jpg"},
	}
	if len(got) != len(cases) {
		t.Fatalf("got %+v", got)
	}
	for field, tc := range cases {
		d := got[field]
		if d.Mode != tc.mode || *d.Old != tc.old || *d.New != tc.new {
			t.Errorf("%s: got %+v", field, d)
		}
	}
}

func TestDiff_ACelebrationTrackKeepsItsAlbumAndGetsTheAlbumVersionLength(t *testing.T) {
	stored := libraryTrack{
		Title: "Celebration", Artist: "Kool & The Gang", Album: "Celebrate!", AlbumArtist: "Kool & The Gang", Year: 1980,
		Genre: "R&B", TrackNumber: 1, CatalogDurationSeconds: 221, ArtworkURL: "https://cdn/other-size.jpg",
	}
	g := group("lp", "Celebrate!", "Album", "1980-10")
	g.Artist = "Kool & The Gang"
	want := derive(anchor{Title: "Celebration", Artist: "Kool & The Gang"}, g, &deezerMatch{CoverURL: "https://cdn/celebrate.jpg", Genre: "Rap", TrackPosition: 3, DurationSeconds: 298})
	got := diff(stored, want)
	if len(got) != 1 || got[0].Field != "catalog_duration_seconds" || got[0].Mode != modeCorrect || *got[0].Old != "221" || *got[0].New != "298" {
		t.Fatalf("got %+v", got)
	}
}

func TestDiff_FillsEmptyFieldsAndNeverInventsDeezerFieldsWithoutAMatch(t *testing.T) {
	stored := libraryTrack{Title: "Careless Whisper", Artist: "Wham!", Album: "Make It Big"}
	got := fields(diff(stored, derive(anchor{Title: "Careless Whisper", Artist: "Wham!"}, makeItBig(), nil)))
	if len(got) != 2 || got["year"].Mode != modeFill || *got["year"].New != "1984" || got["album_artist"].Mode != modeFill {
		t.Fatalf("got %+v", got)
	}
}

func TestDiff_ARemasterYearIsCorrectedToTheOriginalYear(t *testing.T) {
	stored := libraryTrack{Title: "Careless Whisper", Artist: "Wham!", Album: "Make It Big", AlbumArtist: "Wham!", Year: 2024}
	got := diff(stored, derive(anchor{Title: "Careless Whisper", Artist: "Wham!"}, makeItBig(), nil))
	if len(got) != 1 || got[0].Field != "year" || got[0].Mode != modeCorrect || *got[0].Old != "2024" || *got[0].New != "1984" {
		t.Fatalf("got %+v", got)
	}
}

func TestDiff_TitleAndArtistDifferencesAreReportOnly(t *testing.T) {
	stored := libraryTrack{Title: "Wham! - Careless Whisper (Official Video)", Artist: "WhamVEVO", Album: "Make It Big", AlbumArtist: "Wham!", Year: 1984}
	got := diff(stored, derive(anchor{Title: "Careless Whisper", Artist: "Wham!"}, makeItBig(), nil))
	byField := fields(got)
	if len(got) != 2 || byField["title"].Mode != modeReportOnly || byField["artist"].Mode != modeReportOnly {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal([]category{byField["title"].category(), byField["artist"].category()}, []category{"title_differs", "artist_differs"}) {
		t.Fatalf("categories %v", got)
	}
}

func TestDiff_ASameTrackProducesNoDifference(t *testing.T) {
	stored := libraryTrack{
		Title: "Careless Whisper", Artist: "Wham!", Album: "Make It Big (Deluxe)", AlbumArtist: "WHAM!", Year: 1984,
		Genre: "Pop", TrackNumber: 4, CatalogDurationSeconds: 300.4, ArtworkURL: "https://cdn/mib.jpg",
	}
	want := derive(anchor{Title: "Careless Whisper", Artist: "Wham!"}, makeItBig(), &deezerMatch{CoverURL: "https://cdn/mib.jpg", Genre: "Pop", TrackPosition: 4, DurationSeconds: 300})
	if got := diff(stored, want); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
