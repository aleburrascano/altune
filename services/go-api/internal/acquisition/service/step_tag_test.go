package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/acquisition/ports"
)

type fakeTagger struct {
	calls []string
	err   error
}

func (f *fakeTagger) Tag(_ context.Context, filePath string, _ ports.TrackTags) error {
	f.calls = append(f.calls, filePath)
	return f.err
}

func TestTagStep_Execute_NoTempPath_NoOp(t *testing.T) {
	tagger := &fakeTagger{}
	if err := NewTagStep(tagger).Execute(context.Background(), &AcquisitionContext{TempPath: ""}); err != nil {
		t.Fatalf("expected nil for empty temp path, got %v", err)
	}
	if len(tagger.calls) != 0 {
		t.Fatalf("tagger called for empty temp path: %v", tagger.calls)
	}
}

func TestTagStep_Execute_NoTagger_NoOp(t *testing.T) {
	if err := NewTagStep(nil).Execute(context.Background(), &AcquisitionContext{TempPath: "/tmp/x.mp3"}); err != nil {
		t.Fatalf("expected nil without a tagger, got %v", err)
	}
}

// Tagging failures must never fail the pipeline — they are logged and swallowed.
func TestTagStep_Execute_TaggerError_Swallowed(t *testing.T) {
	tagger := &fakeTagger{err: errors.New("boom")}
	ac := &AcquisitionContext{
		Track:    TrackRef{Title: "T", Artist: "A"},
		TempPath: "/tmp/x.mp3",
	}
	if err := NewTagStep(tagger).Execute(context.Background(), ac); err != nil {
		t.Fatalf("expected tagging failure to be swallowed, got %v", err)
	}
}

func TestTagStep_Execute_PassesTrackTags(t *testing.T) {
	var got ports.TrackTags
	tagger := &recordingTagger{tags: &got}
	ac := &AcquisitionContext{
		Track: TrackRef{
			Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours",
			AlbumArtist: "The Weeknd", Genre: "Pop", Year: 2020, TrackNumber: 4,
		},
		TempPath: "/tmp/x.mp3",
	}
	if err := NewTagStep(tagger).Execute(context.Background(), ac); err != nil {
		t.Fatalf("Execute error: %v", err)
	}
	want := ports.TrackTags{
		Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours",
		AlbumArtist: "The Weeknd", Genre: "Pop", Year: 2020, TrackNumber: 4,
	}
	if got != want {
		t.Errorf("tags = %+v, want %+v", got, want)
	}
}

type recordingTagger struct{ tags *ports.TrackTags }

func (r *recordingTagger) Tag(_ context.Context, _ string, tags ports.TrackTags) error {
	*r.tags = tags
	return nil
}
