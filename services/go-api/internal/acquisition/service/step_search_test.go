package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
)

// Issue #980: a search or store step that fails because its context ended must
// be reported as a cancellation, not as the permanent "no match" / "storage
// failed" reason, even when the adapter's error does not wrap ctx.Err().

// cancellingFinder ends the job context mid-search, then fails the way a source
// fan-out does: with an error (or empty result) that drops the ctx cause.
type cancellingFinder struct {
	cancel func()
	err    error
}

func (f *cancellingFinder) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	f.cancel()
	return nil, f.err
}

func TestSearchStep_CancelledMidSearch_ReportsCancellation(t *testing.T) {
	tests := []struct {
		name string
		err  error
	}{
		{"source error without ctx cause", errors.New("all sources failed")},
		{"sources swallowed the cancellation", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			step := NewSearchStep(&cancellingFinder{cancel: cancel, err: tt.err})

			_, err := step.Execute(ctx, &AcquisitionContext{Track: TrackRef{Title: "Song", Artist: "Artist"}}, pipelineStart{})
			if err == nil {
				t.Fatal("expected an error from a cancelled search")
			}
			if got := failureReason(&StepError{Step: "search", Err: err}); got != string(domain.FailureAcquisitionCancelled) {
				t.Errorf("failureReason = %q, want %q", got, domain.FailureAcquisitionCancelled)
			}
		})
	}
}

func TestSearchStep_Execute(t *testing.T) {
	searcher := &fakeAudioSearcher{
		searchResults: []ports.AudioCandidate{
			{
				Title:      "Artist - Song Title",
				Duration:   200,
				URL:        "https://youtube.com/watch?v=abc",
				Channel:    "Artist - Topic",
				Categories: []string{"Music"},
				ViewCount:  1_000_000,
			},
			{
				Title:      "Artist - Song Title (Lyrics)",
				Duration:   201,
				URL:        "https://youtube.com/watch?v=def",
				Channel:    "LyricsChannel",
				Categories: []string{"Music"},
				ViewCount:  500_000,
			},
		},
	}
	step := NewSearchStep(searcher)
	ac := &AcquisitionContext{
		Track: TrackRef{
			Title:  "Song Title",
			Artist: "Artist",
		},
	}

	_, err := step.Execute(context.Background(), ac, pipelineStart{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(ac.Candidates) != 2 {
		t.Fatalf("expected 2 candidates, got %d", len(ac.Candidates))
	}
	if ac.Candidates[0].URL != "https://youtube.com/watch?v=abc" {
		t.Errorf("candidate[0].URL = %q, want %q", ac.Candidates[0].URL, "https://youtube.com/watch?v=abc")
	}
	if ac.Candidates[1].URL != "https://youtube.com/watch?v=def" {
		t.Errorf("candidate[1].URL = %q, want %q", ac.Candidates[1].URL, "https://youtube.com/watch?v=def")
	}
}

func TestSearchStep_Execute_NoCandidates(t *testing.T) {
	searcher := &fakeAudioSearcher{
		searchResults: []ports.AudioCandidate{},
	}
	step := NewSearchStep(searcher)
	ac := &AcquisitionContext{
		Track: TrackRef{
			Title:  "Nonexistent Song",
			Artist: "Nobody",
		},
	}

	_, err := step.Execute(context.Background(), ac, pipelineStart{})

	if err == nil {
		t.Fatal("expected error for no candidates, got nil")
	}
	if got := err.Error(); got != "no candidates found" {
		t.Errorf("error = %q, want %q", got, "no candidates found")
	}
}

func TestSearchStep_Execute_SearchError_NoCandidates(t *testing.T) {
	searcher := &fakeAudioSearcher{
		searchResults: nil,
		searchErr:     fmt.Errorf("network timeout"),
	}
	step := NewSearchStep(searcher)
	ac := &AcquisitionContext{
		Track: TrackRef{
			Title:  "Some Song",
			Artist: "Some Artist",
		},
	}

	_, err := step.Execute(context.Background(), ac, pipelineStart{})

	if err == nil {
		t.Fatal("expected error when all searches fail, got nil")
	}
	if !strings.Contains(err.Error(), "network timeout") {
		t.Errorf("error = %q, want the underlying source failure preserved for the log", err)
	}
	if got := failureReason(&StepError{Step: "search", Err: err}); got != "no_match_found" {
		t.Errorf("client-facing reason = %q, want %q", got, "no_match_found")
	}
}

func TestSearchStep_Execute_DeduplicatesByURL(t *testing.T) {
	searcher := &fakeAudioSearcher{
		searchResults: []ports.AudioCandidate{
			{
				Title:      "Artist - Song",
				Duration:   200,
				URL:        "https://youtube.com/watch?v=same",
				Channel:    "ArtistVEVO",
				Categories: []string{"Music"},
			},
		},
	}
	step := NewSearchStep(searcher)
	ac := &AcquisitionContext{
		Track: TrackRef{
			Title:  "Song",
			Artist: "Artist",
			Album:  "Album",
			ISRC:   "US1234567890",
		},
	}

	_, err := step.Execute(context.Background(), ac, pipelineStart{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ac.Candidates) != 1 {
		t.Errorf("expected 1 deduplicated candidate, got %d", len(ac.Candidates))
	}
}

func TestSearchStep_Name(t *testing.T) {
	step := NewSearchStep(nil)
	if got := step.Name(); got != "search" {
		t.Errorf("Name() = %q, want %q", got, "search")
	}
}
