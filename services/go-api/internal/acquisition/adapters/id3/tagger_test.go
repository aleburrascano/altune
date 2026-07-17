package id3

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"altune/go-api/internal/acquisition/ports"

	"github.com/bogem/id3v2/v2"
)

// Compile-time check: the adapter satisfies acquisition's AudioTagger port.
var _ ports.AudioTagger = (*Tagger)(nil)

// ID3v2 tags are MP3-only; writing them to an m4a/MP4 corrupts the container.
// The tagger must skip non-MP3 files entirely, leaving the bytes untouched.
func TestTagger_SkipsNonMp3(t *testing.T) {
	file := filepath.Join(t.TempDir(), "track.m4a")
	original := []byte("\x00\x00\x00\x1cftypM4A original bytes")
	if err := os.WriteFile(file, original, 0o644); err != nil {
		t.Fatal(err)
	}

	if err := NewTagger().Tag(context.Background(), file, ports.TrackTags{Title: "T", Artist: "A"}); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}

	after, err := os.ReadFile(file)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(original) {
		t.Error("m4a file was modified by the tagger; non-MP3 bytes must be left untouched")
	}
}

// A missing/unreadable file is an error — the pipeline step decides whether to
// swallow it, not the adapter.
func TestTagger_BadPath_ReturnsError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "does-not-exist.mp3")
	if err := NewTagger().Tag(context.Background(), path, ports.TrackTags{Title: "T", Artist: "A"}); err == nil {
		t.Fatal("expected an error for a missing file, got nil")
	}
}

func TestTagger_WritesTags(t *testing.T) {
	file := filepath.Join(t.TempDir(), "track.mp3")
	if err := os.WriteFile(file, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	tags := ports.TrackTags{
		Title:  "Blinding Lights",
		Artist: "The Weeknd",
		Album:  "After Hours",
		Year:   2020,
		Genre:  "Pop",
	}
	if err := NewTagger().Tag(context.Background(), file, tags); err != nil {
		t.Fatalf("Tag error: %v", err)
	}

	tag, err := id3v2.Open(file, id3v2.Options{Parse: true})
	if err != nil {
		t.Fatalf("reopen tagged file: %v", err)
	}
	defer tag.Close()

	if got := tag.Title(); got != "Blinding Lights" {
		t.Errorf("title = %q, want %q", got, "Blinding Lights")
	}
	if got := tag.Artist(); got != "The Weeknd" {
		t.Errorf("artist = %q, want %q", got, "The Weeknd")
	}
	if got := tag.Album(); got != "After Hours" {
		t.Errorf("album = %q, want %q", got, "After Hours")
	}
}
