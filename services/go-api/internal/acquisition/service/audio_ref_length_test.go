package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"
)

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

func TestBuildAudioRef_LongNamesDifferingInTailDoNotCollide(t *testing.T) {
	base := strings.Repeat("x", 200)
	mk := func(tail string) string {
		return BuildAudioRef(TrackRef{UserID: "u1", Artist: "a", Album: "b", Title: base + tail}, "x.mp3")
	}
	if mk("1") == mk("2") {
		t.Fatal("distinct long titles collided")
	}
	if mk("1") != mk("1") {
		t.Fatal("ref is not deterministic")
	}
}
