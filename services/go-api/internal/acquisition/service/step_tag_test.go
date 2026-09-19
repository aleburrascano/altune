package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"errors"
	"strings"
	"testing"
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
	if _, err := NewTagStep(tagger).Execute(context.Background(), &AcquisitionContext{TempPath: ""}, afterDownload{}); err != nil {
		t.Fatalf("expected nil for empty temp path, got %v", err)
	}
	if len(tagger.calls) != 0 {
		t.Fatalf("tagger called for empty temp path: %v", tagger.calls)
	}
}

func TestTagStep_Execute_NoTagger_NoOp(t *testing.T) {
	if _, err := NewTagStep(nil).Execute(context.Background(), &AcquisitionContext{TempPath: "/tmp/x.mp3"}, afterDownload{}); err != nil {
		t.Fatalf("expected nil without a tagger, got %v", err)
	}
}

func TestTagStep_Execute_TaggerError_Swallowed(t *testing.T) {
	tagger := &fakeTagger{err: errors.New("boom")}
	ac := &AcquisitionContext{
		Track:    TrackRef{Title: "T", Artist: "A"},
		TempPath: "/tmp/x.mp3",
	}
	if _, err := NewTagStep(tagger).Execute(context.Background(), ac, afterDownload{}); err != nil {
		t.Fatalf("expected tagging failure to be swallowed, got %v", err)
	}
}

// Issue #1973: the tagger writes to the acquisition temp file and its failures
// name whatever path the OS reports, so this log site needs the same redaction
// the rest of the pipeline's log sites have.
func TestTagStep_Execute_TaggerErrorLogRedactsHostPaths(t *testing.T) {
	logs := captureDefaultLog(t)
	tagger := &fakeTagger{err: errors.New("open /run/secrets/altune/yt_cookies.txt: permission denied")}
	ac := &AcquisitionContext{Track: TrackRef{Title: "T"}, TempPath: "/tmp/x.mp3"}

	if _, err := NewTagStep(tagger).Execute(context.Background(), ac, afterDownload{}); err != nil {
		t.Fatalf("expected tagging failure to be swallowed, got %v", err)
	}

	logged := logs.String()
	if !strings.Contains(logged, "tagging_failed") {
		t.Fatalf("expected the tagging failure log, got:\n%s", logged)
	}
	if strings.Contains(logged, "/run/secrets") {
		t.Fatalf("a host path leaked into the log:\n%s", logged)
	}
	if !strings.Contains(logged, "permission denied") {
		t.Fatalf("redaction dropped the diagnostic text:\n%s", logged)
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
	if _, err := NewTagStep(tagger).Execute(context.Background(), ac, afterDownload{}); err != nil {
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
