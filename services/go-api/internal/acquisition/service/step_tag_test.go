package service

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/bogem/id3v2/v2"
)

func TestTagStep_Execute_NoTempPath_NoOp(t *testing.T) {
	if err := NewTagStep().Execute(context.Background(), &AcquisitionContext{TempPath: ""}); err != nil {
		t.Fatalf("expected nil for empty temp path, got %v", err)
	}
}

// Tagging failures must never fail the pipeline — a missing/unreadable file is
// logged and swallowed.
func TestTagStep_Execute_BadPath_Swallowed(t *testing.T) {
	ac := &AcquisitionContext{
		Track:    TrackRef{Title: "T", Artist: "A"},
		TempPath: filepath.Join(t.TempDir(), "does-not-exist.mp3"),
	}
	if err := NewTagStep().Execute(context.Background(), ac); err != nil {
		t.Fatalf("expected tagging failure to be swallowed, got %v", err)
	}
}

func TestTagStep_Execute_WritesTags(t *testing.T) {
	file := filepath.Join(t.TempDir(), "track.mp3")
	if err := os.WriteFile(file, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}

	ac := &AcquisitionContext{
		Track: TrackRef{
			Title:  "Blinding Lights",
			Artist: "The Weeknd",
			Album:  "After Hours",
			Year:   2020,
			Genre:  "Pop",
		},
		TempPath: file,
	}
	if err := NewTagStep().Execute(context.Background(), ac); err != nil {
		t.Fatalf("Execute error: %v", err)
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
