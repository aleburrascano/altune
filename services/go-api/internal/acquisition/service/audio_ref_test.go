package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"
)

func collisionRef(title string) string {
	return BuildAudioRef(TrackRef{UserID: "u", Artist: "A", Album: "B", Title: title}, "x.mp3")
}

func TestBuildAudioRef_DistinctRecordingsNeverShareAKey(t *testing.T) {
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

func TestBuildAudioRef_VariantsOfOneRecordingShareAKey(t *testing.T) {
	if collisionRef("Café Song") != collisionRef("CAFE song") {
		t.Errorf("case/diacritic variants diverged: %q vs %q", collisionRef("Café Song"), collisionRef("CAFE song"))
	}
}

const testAttemptID = "11111111-1111-1111-1111-111111111111"

func longTracks() map[string]TrackRef {
	return map[string]TrackRef{
		"ascii": {UserID: "u1", Artist: strings.Repeat("a", 300), Album: strings.Repeat("b", 300), Title: strings.Repeat("t", 300)},
		"cjk":   {UserID: "u1", Artist: strings.Repeat("歌", 90), Album: strings.Repeat("集", 90), Title: strings.Repeat("曲", 90)},
	}
}

func TestBuildAudioRef_SegmentsFitFilesystemLimit(t *testing.T) {
	builders := map[string]func(TrackRef, string) string{"canonical": BuildAudioRef, "legacy": BuildLegacyAudioRef}
	for name, track := range longTracks() {
		for builder, build := range builders {
			ref := build(track, "/tmp/x.mp3")
			for _, r := range []string{ref, stagedReplaceRef(ref, testAttemptID)} {
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

func TestBuildAudioRef_LongNamesStoreOnFilesystem(t *testing.T) {
	for name, track := range longTracks() {
		src := filepath.Join(t.TempDir(), "in.mp3")
		if err := os.WriteFile(src, []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(t.TempDir(), stagedReplaceRef(BuildAudioRef(track, src), testAttemptID))
		if err := os.MkdirAll(filepath.Dir(dest), 0o750); err != nil {
			t.Fatalf("%s: mkdir failed: %v", name, err)
		}
		if err := os.WriteFile(dest, []byte("x"), 0o600); err != nil {
			t.Fatalf("%s: write failed: %v", name, err)
		}
	}
}

func TestBuildAudioRef_ShortNamesUnchanged(t *testing.T) {
	track := TrackRef{UserID: "u1", Artist: "Björk", Album: "", Title: "Army of Me"}
	if got, want := BuildLegacyAudioRef(track, "x.flac"), "u1/Björk/Unknown Album/Army of Me.flac"; got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

// TestBuildAudioRef_StripsControlBytesAndInvalidUTF8 feeds a raw control byte
// and an invalid UTF-8 byte through artist/title, both of which arrive
// unescaped from tag-reader metadata. It builds through BuildLegacyAudioRef,
// which calls sanitizePathComponent directly (BuildAudioRef first routes
// through textnorm.NormalizeForMatch, which would mask a regression in
// sanitizePathComponent's own stripping). The ref must still come back valid
// UTF-8 and free of control characters, since it is later used as a
// filesystem path and an HTTP-served object key.
func TestBuildAudioRef_StripsControlBytesAndInvalidUTF8(t *testing.T) {
	track := TrackRef{UserID: "u1", Artist: "Bad\x01Artist", Album: "Album", Title: "Title\xffBytes"}
	ref := BuildLegacyAudioRef(track, "x.mp3")

	if !utf8.ValidString(ref) {
		t.Fatalf("ref is not valid UTF-8: %q", ref)
	}
	for _, r := range ref {
		if unicode.IsControl(r) {
			t.Fatalf("ref %q retains control character %U", ref, r)
		}
	}
}

// TestSanitizePathComponent_ControlCharIsDeterministic pins the stripped
// output for a short name carrying a control byte, and that repeated calls on
// the same input agree, since callers rely on the ref being stable across
// acquisition attempts.
func TestSanitizePathComponent_ControlCharIsDeterministic(t *testing.T) {
	const input = "Ab\x01c"
	const want = "Abc"

	if got := sanitizePathComponent(input); got != want {
		t.Fatalf("sanitizePathComponent(%q) = %q, want %q", input, got, want)
	}
	if got := sanitizePathComponent(input); got != want {
		t.Fatalf("sanitizePathComponent(%q) not deterministic, got %q, want %q", input, got, want)
	}
}

func TestBuildAudioRef_LongNamesDifferingInTailDoNotCollide(t *testing.T) {
	base := strings.Repeat("x", 200)
	mk := func(tail string) string {
		return BuildAudioRef(TrackRef{UserID: "u1", Artist: "a", Album: "b", Title: base + tail}, "x.mp3")
	}
	if mk("1") == mk("2") {
		t.Fatal("distinct long titles collided")
	}
	first, again := mk("1"), mk("1")
	if first != again {
		t.Fatal("ref is not deterministic")
	}
}
