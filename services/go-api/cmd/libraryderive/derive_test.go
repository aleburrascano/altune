package main

import (
	"reflect"
	"slices"
	"strings"
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

func TestDerive_WritesTitleArtistAndAlbumInTheFormTheAppStores(t *testing.T) {
	g := makeItBig()
	g.Title = "Make  It Big "
	got := derive(anchor{Title: " Careless\u00a0Whisper", Artist: "\uff37ham!"}, g, nil)
	if got.Title != "Careless Whisper" || got.Artist != "Wham!" || got.Album != "Make It Big" {
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

func TestDiff_ABobbyMcFerrinTrackStoredOnACompilationCorrectsItsAlbumAndEveryAlbumBoundField(t *testing.T) {
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
		"album":                    {modeCorrect, "Now That's What I Call Music! 14", "Simple Pleasures"},
		"year":                     {modeCorrect, "1999", "1988"},
		"album_artist":             {modeCorrect, "Various Artists", "Bobby McFerrin"},
		"track_number":             {modeCorrect, "12", "2"},
		"genre":                    {modeCorrect, "Pop", "Jazz"},
		"catalog_duration_seconds": {modeCorrect, "240", "292"},
		"artwork_url":              {modeCorrect, "https://cdn/now14.jpg", "https://cdn/sp.jpg"},
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

func TestDiff_ACelebrationTrackKeepsItsAlbumAndHasEveryOtherWrongFieldCorrected(t *testing.T) {
	stored := libraryTrack{
		Title: "Celebration", Artist: "Kool & The Gang", Album: "Celebrate!", AlbumArtist: "Kool & The Gang", Year: 1980,
		Genre: "R&B", TrackNumber: 1, CatalogDurationSeconds: 221, ArtworkURL: "https://cdn/other-size.jpg",
	}
	g := group("lp", "Celebrate!", "Album", "1980-10")
	g.Artist = "Kool & The Gang"
	want := derive(anchor{Title: "Celebration", Artist: "Kool & The Gang"}, g, &deezerMatch{CoverURL: "https://cdn/celebrate.jpg", Genre: "Rap", TrackPosition: 3, DurationSeconds: 298})
	got := fields(diff(stored, want))
	cases := map[string][2]string{
		"track_number":             {"1", "3"},
		"genre":                    {"R&B", "Rap"},
		"catalog_duration_seconds": {"221", "298"},
		"artwork_url":              {"https://cdn/other-size.jpg", "https://cdn/celebrate.jpg"},
	}
	if len(got) != len(cases) {
		t.Fatalf("got %+v", got)
	}
	for field, tc := range cases {
		d := got[field]
		if d.Mode != modeCorrect || d.Group != "" || *d.Old != tc[0] || *d.New != tc[1] {
			t.Errorf("%s: got %+v", field, d)
		}
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

func TestDiff_TitleAndArtistDifferencesAreCorrectFixes(t *testing.T) {
	stored := libraryTrack{Title: "Wham! - Careless Whisper (Official Video)", Artist: "WhamVEVO", Album: "Make It Big", AlbumArtist: "Wham!", Year: 1984}
	got := diff(stored, derive(anchor{Title: "Careless Whisper", Artist: "Wham!"}, makeItBig(), nil))
	byField := fields(got)
	if len(got) != 2 || byField["title"].Mode != modeCorrect || byField["artist"].Mode != modeCorrect {
		t.Fatalf("got %+v", got)
	}
	if !slices.Equal([]category{byField["title"].category(), byField["artist"].category()}, []category{"title_correct", "artist_correct"}) {
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

func TestDiff_AnAlbumMoveIsACorrectFixWhoseDependentsShareItsGroup(t *testing.T) {
	stored := libraryTrack{
		ID: "t1", Title: "Don't Worry, Be Happy", Artist: "Bobby McFerrin", Album: "Now That's What I Call Music! 14", AlbumArtist: "Various Artists",
		ArtworkURL: "https://cdn/now14.jpg", Year: 1999, Genre: "Pop", TrackNumber: 12, CatalogDurationSeconds: 240,
	}
	g := group("lp", "Simple Pleasures", "Album", "1988-06-01")
	g.Artist = "Bobby McFerrin"
	want := derive(anchor{Title: "Don't Worry, Be Happy", Artist: "Bobby McFerrin"}, g,
		&deezerMatch{CoverURL: "https://cdn/sp.jpg", Genre: "Jazz", TrackPosition: 2, DurationSeconds: 292})
	got := fields(diff(stored, want))
	grouped := []string{"album", "year", "album_artist", "track_number", "genre", "artwork_url"}
	for _, field := range grouped {
		if got[field].Mode != modeCorrect || got[field].Group != "album-move:t1" {
			t.Errorf("%s: got %+v", field, got[field])
		}
	}
	if length := got["catalog_duration_seconds"]; len(got) != len(grouped)+1 || length.Mode != modeCorrect || length.Group != "" {
		t.Errorf("the catalog length stays outside the group: %+v", got)
	}
}

func TestDiff_AGroupOnlyExistsAlongsideAnAlbumFix(t *testing.T) {
	stored := libraryTrack{ID: "t1", Title: "Careless Whisper", Artist: "Wham!", Album: "Make It Big", AlbumArtist: "Wham!", Year: 2024, Genre: "Pop"}
	got := diff(stored, derive(anchor{Title: "Careless Whisper", Artist: "Wham!"}, makeItBig(), &deezerMatch{Genre: "Rock"}))
	for _, d := range got {
		if d.Group != "" {
			t.Errorf("%s carries group %q without an album move", d.Field, d.Group)
		}
	}
}

func TestDiff_AMovedAlbumCarriesTheTitleAndArtistCorrectionsInItsGroup(t *testing.T) {
	stored := libraryTrack{ID: "t1", Title: "Wham! - Careless Whisper (Official Video)", Artist: "WhamVEVO", Album: "Greatest Hits", AlbumArtist: "Wham!", Year: 1984}
	got := fields(diff(stored, derive(anchor{Title: "Careless Whisper", Artist: "Wham!"}, makeItBig(), nil)))
	for _, field := range []string{"album", "title", "artist"} {
		if got[field].Mode != modeCorrect || got[field].Group != "album-move:t1" {
			t.Errorf("%s: got %+v", field, got[field])
		}
	}
}

func TestDiff_ATrackWithNoAlbumGetsTheAlbumFilledAndItsDependentsWrittenUngrouped(t *testing.T) {
	stored := libraryTrack{ID: "t1", Title: "Careless Whisper", Artist: "Wham!", AlbumArtist: "Wham!", Year: 1984, Genre: "Pop"}
	got := fields(diff(stored, derive(anchor{Title: "Careless Whisper", Artist: "Wham!"}, makeItBig(), &deezerMatch{Genre: "Rock", TrackPosition: 4})))
	want := map[string]changeMode{"album": modeFill, "genre": modeCorrect}
	for field, mode := range want {
		if got[field].Mode != mode || got[field].Group != "" {
			t.Errorf("%s: got %+v", field, got[field])
		}
	}
}

func TestDiff_AMissingCoverIsFilledWithTheChosenReleaseCover(t *testing.T) {
	stored := libraryTrack{ID: "t1", Title: "Careless Whisper", Artist: "Wham!", Album: "Make It Big", AlbumArtist: "Wham!", Year: 1984}
	got := fields(diff(stored, derive(anchor{Title: "Careless Whisper", Artist: "Wham!"}, makeItBig(), &deezerMatch{CoverURL: "https://cdn/mib.jpg"})))
	if d := got["artwork_url"]; d.Mode != modeFill || d.Old != nil || *d.New != "https://cdn/mib.jpg" {
		t.Fatalf("got %+v", got)
	}
}

func TestDiff_ATrackOnItsOwnAlbumWithTheSinglesCoverAndPositionIsCorrectedWithoutAGroup(t *testing.T) {
	stored := libraryTrack{
		Title: "Body", Artist: "Don Toliver", Album: "OCTANE", AlbumArtist: "Various Artists", Year: 2026, Genre: "Hip-Hop/Rap",
		TrackNumber: 1, ArtworkURL: "https://cdn/body-single.jpg",
	}
	octane := group("lp", "OCTANE", "Album", "2026-01")
	octane.Artist = "Don Toliver"
	match := &deezerMatch{CoverURL: "https://cdn/octane.jpg", Genre: "Hip-Hop/Rap", TrackPosition: 7}
	got := fields(diff(stored, derive(anchor{Title: "Body", Artist: "Don Toliver"}, octane, match)))
	want := map[string][2]string{
		"artwork_url": {"https://cdn/body-single.jpg", "https://cdn/octane.jpg"}, "track_number": {"1", "7"}, "album_artist": {"Various Artists", "Don Toliver"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %+v", got)
	}
	for field, values := range want {
		if d := got[field]; d.Mode != modeCorrect || d.Group != "" || *d.Old != values[0] || *d.New != values[1] {
			t.Errorf("%s: got %+v", field, d)
		}
	}
}

func TestDiff_ABoundTracksVariantTitleEmptyCoverAndWrongISRCAreAllWritten(t *testing.T) {
	stored := libraryTrack{ID: "t1", Title: "Careless Whisper", Artist: "Wham!", Album: "Make It Big", AlbumArtist: "Wham!", Year: 1984, ISRC: "GBAYE8400009"}
	want := derive(anchor{ISRC: "GBAYE8400001", Title: "Careless Whisper (Live)", Artist: "Wham!"}, makeItBig(), &deezerMatch{CoverURL: "https://cdn/mib.jpg"})
	got := fields(diff(stored, want))
	if d := got["title"]; d.Mode != modeCorrect || *d.Old != "Careless Whisper" || *d.New != "Careless Whisper (Live)" {
		t.Errorf("title: %+v", d)
	}
	if d := got["artwork_url"]; d.Mode != modeFill || d.Old != nil || *d.New != "https://cdn/mib.jpg" {
		t.Errorf("artwork_url: %+v", d)
	}
	d := got["isrc"]
	if d.Mode != modeCorrect || *d.Old != "GBAYE8400009" || *d.New != "GBAYE8400001" {
		t.Errorf("isrc: %+v", d)
	}
	if f := findingOf(boundTrack{libraryTrack: stored}, d); f.Fix == nil || f.Fix.Sources[0] != sourceTrackIdentity {
		t.Errorf("isrc finding: %+v", f)
	}
}

func TestDiff_ADeluxeEditionOfTheChosenAlbumKeepsItsOwnPositionAndCover(t *testing.T) {
	stored := libraryTrack{Title: "Body", Artist: "Don Toliver", Album: "OCTANE (Deluxe)", AlbumArtist: "Don Toliver", Year: 2026, TrackNumber: 14, ArtworkURL: "https://cdn/deluxe.jpg"}
	octane := group("lp", "OCTANE", "Album", "2026-01")
	octane.Artist = "Don Toliver"
	match := &deezerMatch{CoverURL: "https://cdn/octane.jpg", TrackPosition: 7}
	got := fields(diff(stored, derive(anchor{Title: "Body", Artist: "Don Toliver"}, octane, match)))
	for _, field := range []string{"track_number", "artwork_url"} {
		if d := got[field]; d.Mode != modeReportOnly || !strings.Contains(d.Reason, "another edition") {
			t.Errorf("%s: got %+v", field, d)
		}
	}
}

type fieldCase struct {
	diffField string
	differ    func(stored *libraryTrack, want *desired)
	empty     func(want *desired)
}

func settledTrack() (libraryTrack, desired) {
	stored := libraryTrack{
		ID: "t1", Title: "Song", Artist: "Artist", Album: "Album", AlbumArtist: "Artist", ISRC: "GBAYE8400001", ArtworkURL: "https://cdn/a.jpg",
		Year: 2000, Genre: "Pop", TrackNumber: 3, CatalogDurationSeconds: 200,
	}
	want := desired{
		Title: "Song", Artist: "Artist", Album: "Album", AlbumArtist: "Artist", ISRC: "GBAYE8400001", ArtworkURL: "https://cdn/a.jpg",
		Year: 2000, Genre: "Pop", TrackNumber: 3, CatalogDurationSeconds: 200,
	}
	return stored, want
}

var desiredFieldCases = map[string]fieldCase{
	"Title":       {"title", func(_ *libraryTrack, w *desired) { w.Title = "Other Song" }, func(w *desired) { w.Title = "" }},
	"Artist":      {"artist", func(_ *libraryTrack, w *desired) { w.Artist = "Someone Else" }, func(w *desired) { w.Artist = "" }},
	"Album":       {"album", func(s *libraryTrack, _ *desired) { s.Album = "Old Album" }, func(w *desired) { w.Album = "" }},
	"AlbumArtist": {"album_artist", func(_ *libraryTrack, w *desired) { w.AlbumArtist = "Someone Else" }, func(w *desired) { w.AlbumArtist = "" }},
	"Year":        {"year", func(_ *libraryTrack, w *desired) { w.Year = 2001 }, func(w *desired) { w.Year = 0 }},
	"TrackNumber": {"track_number", func(_ *libraryTrack, w *desired) { w.TrackNumber = 4 }, func(w *desired) { w.TrackNumber = 0 }},
	"Genre":       {"genre", func(_ *libraryTrack, w *desired) { w.Genre = "Rap" }, func(w *desired) { w.Genre = "" }},
	"CatalogDurationSeconds": {
		"catalog_duration_seconds", func(_ *libraryTrack, w *desired) { w.CatalogDurationSeconds = 250 }, func(w *desired) { w.CatalogDurationSeconds = 0 },
	},
	"ArtworkURL": {"artwork_url", func(_ *libraryTrack, w *desired) { w.ArtworkURL = "https://cdn/b.jpg" }, func(w *desired) { w.ArtworkURL = "" }},
	"ISRC":       {"isrc", func(_ *libraryTrack, w *desired) { w.ISRC = "GBAYE8400002" }, func(w *desired) { w.ISRC = "" }},
	"WhyNot":     {},
}

func TestDiff_EveryFieldOfDesiredEndsInAFillACorrectionOrAnExplainedUnknown(t *testing.T) {
	stored, want := settledTrack()
	if got := diff(stored, want); len(got) != 0 {
		t.Fatalf("a settled track differs: %+v", got)
	}
	desiredType := reflect.TypeOf(desired{})
	for i := range desiredType.NumField() {
		name := desiredType.Field(i).Name
		c, handled := desiredFieldCases[name]
		if !handled {
			t.Fatalf("desired.%s is not handled by diff: add a case to desiredFieldCases and to diff", name)
		}
		if c.diffField == "" {
			continue
		}
		t.Run(name+"/differs", func(t *testing.T) {
			stored, want := settledTrack()
			c.differ(&stored, &want)
			got := diff(stored, want)
			if len(got) != 1 || got[0].Field != c.diffField || (got[0].Mode != modeFill && got[0].Mode != modeCorrect) {
				t.Fatalf("got %+v", got)
			}
		})
		t.Run(name+"/untrusted", func(t *testing.T) {
			stored, want := settledTrack()
			c.empty(&want)
			got := diff(stored, want)
			if len(got) != 1 || got[0].Field != c.diffField || got[0].Mode != modeReportOnly || got[0].Reason == "" {
				t.Fatalf("got %+v", got)
			}
		})
	}
}

func TestDiff_AnUnknownCarriesTheReasonDeriveGaveForIt(t *testing.T) {
	stored, want := settledTrack()
	want.ArtworkURL, want.WhyNot = "", withheld{ArtworkURL: "deezer cover is the blank placeholder"}
	got := diff(stored, want)
	if len(got) != 1 || got[0].Reason != "deezer cover is the blank placeholder" {
		t.Fatalf("got %+v", got)
	}
}

func TestDiff_AStoredCoverEqualToTheDerivedOneNeverProducesAFixEvenWhenItIsFlaggedBlank(t *testing.T) {
	stored, want := settledTrack()
	stored.CoverIsPlaceholder = true
	if got := diff(stored, want); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}
