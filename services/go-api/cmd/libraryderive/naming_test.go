package main

import "testing"

func TestSameSongTitle(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"a film name in brackets does not count", "Sunflower", "Sunflower (Spider-Man: Into the Spider-Verse)", true},
		{"a feat credit does not count", "Song", "Song (feat. Someone)", true},
		{"case does not count", "SONG", "song", true},
		{"a remix qualifier counts", "Song", "Song (Remix)", false},
		{"a slowed qualifier counts", "Song", "Song (Slowed)", false},
		{"equal qualifiers agree", "Song (Live)", "Song - Live", true},
		{"another song", "Lose Yourself", "Just Lose It", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := sameSongTitle(tc.a, tc.b); got != tc.want {
				t.Fatalf("sameSongTitle(%q, %q) = %v", tc.a, tc.b, got)
			}
		})
	}
}

func TestIsCredited(t *testing.T) {
	cases := []struct {
		stored, credit string
		want           bool
	}{
		{"Post Malone", "Post Malone & Swae Lee", true},
		{"Nitefreak", "NiteFreak + Emmanuel Jal", true},
		{"ChaseTheMoney", "Lucki & CHASETHEMONEY", true},
		{"Kool & The Gang", "Kool & The Gang", true},
		{"Wham!", "Wham! feat. George Michael", true},
		{"George Michael", "Wham! feat. George Michael", true},
		{"WhamVEVO", "Wham!", false},
		{"Various Artists", "Bobby McFerrin", false},
		{"", "Bobby McFerrin", false},
	}
	for _, tc := range cases {
		if got := isCredited(tc.stored, tc.credit); got != tc.want {
			t.Errorf("isCredited(%q, %q) = %v", tc.stored, tc.credit, got)
		}
	}
}

func TestChooseRelease_ASingleThatOnlyCarriesTheSongAsABSideIsNoHome(t *testing.T) {
	loseYourself := []releaseGroup{
		group("ost", "8 Mile", "Album", "2002", "Soundtrack"),
		group("hits", "Curtain Call", "Album", "2005", "Compilation"),
		group("bside", "Just Lose It", "Single", "2004-09-27"),
	}
	if got, ok := chooseRelease(ownSingles(loseYourself, "Lose Yourself")); ok {
		t.Fatalf("picked %+v", got)
	}
}

func TestChooseRelease_ASingleWithTheSongsOwnTitleStillWins(t *testing.T) {
	groups := []releaseGroup{
		group("bside", "Other Song", "Single", "1983"),
		group("own", "Careless Whisper (2024 Remaster)", "Single", "1984-07-24"),
	}
	got, ok := chooseRelease(ownSingles(groups, "Careless Whisper"))
	if !ok || got.Group.ID != "own" || got.Rule != ruleSingleOrEP {
		t.Fatalf("got %+v ok=%v", got, ok)
	}
}

func TestChooseRelease_ALoneBSideSingleIsNotAnOnlyHome(t *testing.T) {
	if got, ok := chooseRelease(ownSingles([]releaseGroup{group("bside", "Just Lose It", "Single", "2004")}, "Lose Yourself")); ok {
		t.Fatalf("picked %+v", got)
	}
}

func TestDiff_TheStoredTitleAndArtistSurviveFeatCreditsFilmNamesAndSharedCredits(t *testing.T) {
	stored := libraryTrack{Title: "Sunflower", Artist: "Post Malone", Album: "Hollywood's Bleeding", AlbumArtist: "post malone & swae lee", Year: 2018}
	g := group("lp", "Hollywood's Bleeding", "Album", "2018")
	g.Artist = "Post Malone & Swae Lee"
	want := derive(anchor{Title: "Sunflower (Spider-Man: Into the Spider-Verse)", Artist: "Post Malone & Swae Lee"}, g, nil)
	if got := diff(stored, want); len(got) != 0 {
		t.Fatalf("got %+v", got)
	}
}

func TestDiff_AnAlbumArtistAbsentFromTheCreditIsStillCorrected(t *testing.T) {
	stored := libraryTrack{Title: "Song", Artist: "A", Album: "LP", AlbumArtist: "Various Artists", Year: 2000}
	g := group("lp", "LP", "Album", "2000")
	g.Artist = "A & B"
	got := diff(stored, derive(anchor{Title: "Song", Artist: "A & B"}, g, nil))
	if len(got) != 1 || got[0].Field != "album_artist" || got[0].Mode != modeCorrect {
		t.Fatalf("got %+v", got)
	}
}

func TestDiff_ATitleVariantIsCorrectedToTheBoundRecordingTitle(t *testing.T) {
	stored := libraryTrack{Title: "Song (Slowed)", Artist: "A", Album: "LP", AlbumArtist: "A", Year: 2000}
	g := group("lp", "LP", "Album", "2000")
	g.Artist = "A"
	got := diff(stored, derive(anchor{Title: "Song", Artist: "A"}, g, nil))
	if len(got) != 1 || got[0].category() != "title_correct" || got[0].Mode != modeCorrect || *got[0].Old != "Song (Slowed)" || *got[0].New != "Song" {
		t.Fatalf("got %+v", got)
	}
}

func TestDiff_AnAlbumArtistThatIsOnlyPartOfTheCreditIsCorrectedToTheWholeCredit(t *testing.T) {
	stored := libraryTrack{Title: "Song", Artist: "Simon", Album: "LP", AlbumArtist: "Simon", Year: 2000}
	g := group("lp", "LP", "Album", "2000")
	g.Artist = "Simon and Garfunkel"
	got := diff(stored, derive(anchor{Title: "Song", Artist: "Simon and Garfunkel"}, g, nil))
	if len(got) != 1 || got[0].Field != "album_artist" || got[0].Mode != modeCorrect || *got[0].New != "Simon and Garfunkel" {
		t.Fatalf("got %+v", got)
	}
}

func TestIsVariousArtists(t *testing.T) {
	for _, name := range []string{"Various Artists", "various artists", "VA", "Varios Arti" + "stas", "Verschiedene Interpreten"} {
		if !isVariousArtists(name) {
			t.Errorf("%q is various artists", name)
		}
	}
	if isVariousArtists("Vampire Weekend") {
		t.Error("a band is not various artists")
	}
}
