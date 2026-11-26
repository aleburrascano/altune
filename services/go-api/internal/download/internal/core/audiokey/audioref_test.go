package audiokey_test

import (
	"altune/go-api/internal/download/internal/core/audiokey"
	"altune/go-api/internal/shared/ids"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func collisionRef(title string) string {
	return audiokey.Build(audiokey.Names{UserID: "u", Artist: "A", Album: "B", Title: title}, "x.mp3")
}

func TestBuild_DistinctRecordingsNeverShareAKey(t *testing.T) {
	distinct := [][2]string{
		{"Song", "Song (Live)"},
		{"Song (Live)", "Song (Demo)"},
		{"!!!", "???"},
		{"(Untitled)", "!!!"},
	}
	for _, pair := range distinct {
		if collisionRef(pair[0]) == collisionRef(pair[1]) {
			t.Errorf("%q and %q share key %q", pair[0], pair[1], collisionRef(pair[0]))
		}
	}
}

func TestBuild_VariantsOfOneRecordingShareAKey(t *testing.T) {
	if collisionRef("Café Song") != collisionRef("CAFE song") {
		t.Errorf("case/diacritic variants diverged: %q vs %q", collisionRef("Café Song"), collisionRef("CAFE song"))
	}
}

func legacySegment(s string) string {
	return strings.Split(audiokey.BuildLegacy(audiokey.Names{UserID: "u", Artist: s, Album: "b", Title: "t"}, "x.mp3"), "/")[1]
}

const testAttemptID = "11111111-1111-1111-1111-111111111111"

func longTracks() map[string]audiokey.Names {
	return map[string]audiokey.Names{
		"ascii": {UserID: "u1", Artist: strings.Repeat("a", 300), Album: strings.Repeat("b", 300), Title: strings.Repeat("t", 300)},
		"cjk":   {UserID: "u1", Artist: strings.Repeat("歌", 90), Album: strings.Repeat("集", 90), Title: strings.Repeat("曲", 90)},
	}
}

func TestBuild_SegmentsFitFilesystemLimit(t *testing.T) {
	builders := map[string]func(audiokey.Names, string) string{"canonical": audiokey.Build, "legacy": audiokey.BuildLegacy}
	for name, track := range longTracks() {
		for builder, build := range builders {
			ref := build(track, "/tmp/x.mp3")
			for _, r := range []string{ref, audiokey.StagedReplace(ref, testAttemptID)} {
				if !utf8.ValidString(r) {
					t.Fatalf("%s/%s: invalid UTF-8 in %q", name, builder, r)
				}
				for _, seg := range strings.Split(r, "/") {
					if len(seg) > 255 {
						t.Fatalf("%s/%s: segment of %d bytes", name, builder, len(seg))
					}
				}
			}
		}
	}
}

func TestBuild_LongNamesStoreOnFilesystem(t *testing.T) {
	for name, track := range longTracks() {
		src := filepath.Join(t.TempDir(), "in.mp3")
		if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(t.TempDir(), audiokey.StagedReplace(audiokey.Build(track, src), testAttemptID))
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			t.Fatalf("%s: mkdir failed: %v", name, err)
		}
		if err := os.WriteFile(dest, []byte("x"), 0o600); err != nil {
			t.Fatalf("%s: write failed: %v", name, err)
		}
	}
}

func TestBuild_ShortNamesUnchanged(t *testing.T) {
	track := audiokey.Names{UserID: "u1", Artist: "Björk", Album: "", Title: "Army of Me"}
	if got, want := audiokey.BuildLegacy(track, "x.flac"), "u1/Björk/Unknown Album/Army of Me.flac"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestBuild_StripsControlBytesAndInvalidUTF8(t *testing.T) {
	track := audiokey.Names{UserID: "u1", Artist: "Bad\x01Artist", Album: "Album", Title: "Title\xffBytes"}
	ref := audiokey.BuildLegacy(track, "x.mp3")

	if !utf8.ValidString(ref) {
		t.Fatalf("ref is not valid UTF-8: %q", ref)
	}
	for _, r := range ref {
		if unicode.IsControl(r) {
			t.Fatalf("ref %q retains control character %U", ref, r)
		}
	}
}

func TestBuildLegacy_DropsAControlCharDeterministically(t *testing.T) {
	const input = "Ab\x01c"
	const want = "Abc"

	if got := legacySegment(input); got != want {
		t.Fatalf("the legacy segment of %q = %q, want %q", input, got, want)
	}
	if got := legacySegment(input); got != want {
		t.Fatalf("the legacy segment of %q not deterministic, got %q, want %q", input, got, want)
	}
}

func TestBuild_LongNamesDifferingInTailDoNotCollide(t *testing.T) {
	base := strings.Repeat("x", 200)
	mk := func(tail string) string {
		return audiokey.Build(audiokey.Names{UserID: "u1", Artist: "a", Album: "b", Title: base + tail}, "x.mp3")
	}
	if mk("1") == mk("2") {
		t.Fatal("distinct long titles collided")
	}
	first, again := mk("1"), mk("1")
	if first != again {
		t.Fatal("ref is not deterministic")
	}
}

func TestBuild(t *testing.T) {
	tests := []struct {
		name  string
		track audiokey.Names
		want  string
	}{
		{
			name: "normal track",
			track: audiokey.Names{
				UserID: "uid",
				Artist: "The Weeknd",
				Album:  "After Hours",
				Title:  "Blinding Lights",
			},
			want: "uid/the weeknd/after hours/blinding lights.mp3",
		},
		{
			name: "empty album defaults to Unknown Album",
			track: audiokey.Names{
				UserID: "uid",
				Artist: "Artist",
				Album:  "",
				Title:  "Song",
			},
			want: "uid/artist/unknown album/song.mp3",
		},
		{
			name: "forbidden chars stripped and case/unicode normalized",
			track: audiokey.Names{
				UserID: "uid",
				Artist: "AC/DC",
				Album:  `The "Best" Album`,
				Title:  "Song: Title?",
			},
			want: "uid/ac dc/the best album/song title.mp3",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := audiokey.Build(tt.track, "/tmp/acquire/downloaded.mp3")
			if got != tt.want {
				t.Errorf("Build() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestBuildLegacy_SanitizesEachSegment(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "empty string", input: "", want: "Unknown"},
		{name: "all forbidden chars", input: `<>:"/\|?*;`, want: "Unknown"},
		{name: "normal string", input: "Hello World", want: "Hello World"},
		{name: "mixed content", input: `Song: "Title"`, want: "Song Title"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := legacySegment(tt.input)
			if got != tt.want {
				t.Errorf("the legacy segment of %q = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestTrackScoped_PutsTheTrackIDBeforeTheExtension(t *testing.T) {
	id := ids.NewTrackId()
	got := audiokey.TrackScoped("u/a/b/song.opus", id)
	if want := "u/a/b/song." + id.String() + ".opus"; got != want {
		t.Errorf("TrackScoped = %q, want %q", got, want)
	}
}

func TestTrackScoped_IgnoresDotsInTheDirectories(t *testing.T) {
	id := ids.NewTrackId()
	got := audiokey.TrackScoped("staging.v2/u/mr. big/x/to be with you.opus", id)
	if want := "staging.v2/u/mr. big/x/to be with you." + id.String() + ".opus"; got != want {
		t.Errorf("TrackScoped = %q, want %q", got, want)
	}
}

func legacyTitle(title string) string {
	ref := audiokey.BuildLegacy(audiokey.Names{UserID: "u", Artist: "a", Album: "b", Title: title}, "x.mp3")
	return strings.TrimSuffix(strings.Split(ref, "/")[3], ".mp3")
}

func TestBuildLegacy_ASegmentOfExactlyTheCapIsKeptWhole(t *testing.T) {
	title := strings.Repeat("t", 120)
	if got := legacyTitle(title); got != title {
		t.Errorf("segment = %q, want the 120-byte title unchanged", got)
	}
}

func TestBuildLegacy_ASegmentOverTheCapKeepsItsHeadAndEndsInAHash(t *testing.T) {
	title := strings.Repeat("t", 121)

	got := legacyTitle(title)

	if len(got) != 120 {
		t.Fatalf("segment = %q (%d bytes), want exactly 120", got, len(got))
	}
	head, hash, ok := strings.Cut(got, "-")
	if !ok || head != title[:111] || len(hash) != 8 || strings.Trim(hash, "0123456789abcdef") != "" {
		t.Errorf("segment = %q, want the first 111 bytes, a dash and 8 hex digits", got)
	}
}

func TestBuildLegacy_ACapThatFallsInsideACharacterCutsBeforeIt(t *testing.T) {
	title := "a" + strings.Repeat("歌", 60)

	got := legacyTitle(title)

	head, _, ok := strings.Cut(got, "-")
	if !ok || head != title[:109] {
		t.Errorf("segment = %q, want the 109 bytes before the character the cap splits", got)
	}
}
