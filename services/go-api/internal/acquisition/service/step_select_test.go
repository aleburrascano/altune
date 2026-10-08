package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"errors"
	"testing"
)

func TestSelectStep_SkipTopRankedDropsTheLeader(t *testing.T) {
	ac := &AcquisitionContext{
		Track:   TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		Replace: ReplaceState{SkipTopRanked: true},
		Candidates: []ports.AudioCandidate{
			{Title: "Blinding Lights", URL: "leader", Channel: "The Weeknd - Topic", Duration: 200},
			{Title: "The Weeknd - Blinding Lights", URL: "runner-up", Channel: "Someone", Duration: 201},
		},
	}

	if _, err := NewSelectStep().Execute(context.Background(), ac, afterSearch{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ac.Selected == nil || ac.Selected.URL != "runner-up" {
		t.Fatalf("selected = %+v, want the runner-up", ac.Selected)
	}
}

func TestSelectStep_SkipTopRankedWithOneCandidateFails(t *testing.T) {
	ac := &AcquisitionContext{
		Track:   TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		Replace: ReplaceState{SkipTopRanked: true},
		Candidates: []ports.AudioCandidate{
			{Title: "Blinding Lights", URL: "only", Channel: "The Weeknd - Topic", Duration: 200},
		},
	}

	if _, err := NewSelectStep().Execute(context.Background(), ac, afterSearch{}); err == nil {
		t.Fatal("expected failure rather than re-storing the only candidate")
	}
}

func TestSelectStep_SkipTopRankedOffByDefault(t *testing.T) {
	ac := &AcquisitionContext{
		Track: TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		Candidates: []ports.AudioCandidate{
			{Title: "Blinding Lights", URL: "leader", Channel: "The Weeknd - Topic", Duration: 200},
		},
	}

	if _, err := NewSelectStep().Execute(context.Background(), ac, afterSearch{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if ac.Selected == nil || ac.Selected.URL != "leader" {
		t.Fatalf("an ordinary acquire must keep the leader, got %+v", ac.Selected)
	}
}

func TestSelectStep_Execute(t *testing.T) {
	step := NewSelectStep()
	ac := &AcquisitionContext{
		Track: TrackRef{
			Title:    "Blinding Lights",
			Artist:   "The Weeknd",
			Duration: 200,
		},
		Candidates: []ports.AudioCandidate{
			{
				Title:      "Blinding Lights",
				Channel:    "The Weeknd - Topic",
				Duration:   200,
				URL:        "https://youtube.com/watch?v=topic1",
				Categories: []string{"Music"},
				ViewCount:  10_000_000,
			},
		},
	}

	_, err := step.Execute(context.Background(), ac, afterSearch{})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if ac.Selected == nil {
		t.Fatal("expected ac.Selected to be populated, got nil")
	}
	if ac.Selected.URL != "https://youtube.com/watch?v=topic1" {
		t.Errorf("Selected.URL = %q, want %q", ac.Selected.URL, "https://youtube.com/watch?v=topic1")
	}
}

func TestSelectStep_Execute_NoCandidates(t *testing.T) {
	step := NewSelectStep()
	ac := &AcquisitionContext{
		Track:      TrackRef{Title: "Song", Artist: "Artist", Duration: 200},
		Candidates: []ports.AudioCandidate{},
	}

	_, err := step.Execute(context.Background(), ac, afterSearch{})

	if err == nil {
		t.Fatal("expected error for no candidates passing gates, got nil")
	}
	if got := err.Error(); got != "no candidates passed matching gates" {
		t.Errorf("error = %q, want %q", got, "no candidates passed matching gates")
	}
}

func TestSelectStep_Execute_AllCandidatesBelowThreshold(t *testing.T) {
	step := NewSelectStep()
	ac := &AcquisitionContext{
		Track: TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		Candidates: []ports.AudioCandidate{
			{
				Title:      "Cooking Tutorial Episode 47",
				Channel:    "CookingChannel",
				Duration:   200,
				URL:        "https://youtube.com/watch?v=cook1",
				Categories: []string{"Howto & Style"},
				ViewCount:  50_000,
			},
		},
	}

	_, err := step.Execute(context.Background(), ac, afterSearch{})

	if err == nil {
		t.Fatal("expected error when all candidates are below identity threshold, got nil")
	}
}

func TestSelectStep_Name(t *testing.T) {
	step := NewSelectStep()
	if got := step.Name(); got != "select" {
		t.Errorf("Name() = %q, want %q", got, "select")
	}
}

func searchOutageThenSelect(t *testing.T, down findOnlySource, healthy findOnlySource, replace ReplaceState) (context.Context, error) {
	t.Helper()
	ctx := withJobAttempt(context.Background(), 1)
	ac := &AcquisitionContext{
		Track:   TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", Duration: 200},
		Replace: replace,
	}
	if _, err := NewSearchStep(NewSourceRegistry(down, healthy)).Execute(ctx, ac, pipelineStart{}); err != nil {
		t.Fatalf("search: %v", err)
	}
	_, err := NewSelectStep().Execute(ctx, ac, afterSearch{})
	if err == nil {
		t.Fatal("expected a select error")
	}
	return ctx, err
}

var (
	rejectedCandidate = ports.AudioCandidate{Title: "Cooking Tutorial Episode 47", Channel: "CookingChannel", Duration: 20, URL: "https://example.com/cook"}
	matchingCandidate = ports.AudioCandidate{Title: "Blinding Lights", Channel: "The Weeknd - Topic", Duration: 200, URL: "https://example.com/match"}
)

func outageSource() findOnlySource {
	return findOnlySource{name: "down", err: &ports.SourceUnavailableError{Source: "down", Err: errors.New("429")}}
}

func TestSelectStep_SearchOutageWithAllRejected_IsRetryable(t *testing.T) {
	healthy := findOnlySource{name: "healthy", candidates: []ports.AudioCandidate{rejectedCandidate}}

	ctx, err := searchOutageThenSelect(t, outageSource(), healthy, ReplaceState{})

	if !ports.IsSourceUnavailable(err) || !willRetry(ctx, err) {
		t.Fatalf("err = %v, want a retryable source-unavailable failure", err)
	}
}

func TestSelectStep_SearchOutageWithSkipTopRanked_IsSourceUnavailable(t *testing.T) {
	healthy := findOnlySource{name: "healthy", candidates: []ports.AudioCandidate{matchingCandidate}}

	_, err := searchOutageThenSelect(t, outageSource(), healthy, ReplaceState{SkipTopRanked: true})

	if !ports.IsSourceUnavailable(err) {
		t.Fatalf("err = %v, want source-unavailable", err)
	}
}

func TestSelectStep_PlainSourceErrorWithAllRejected_StaysPermanent(t *testing.T) {
	down := findOnlySource{name: "down", err: errors.New("exit 1")}
	healthy := findOnlySource{name: "healthy", candidates: []ports.AudioCandidate{rejectedCandidate}}

	ctx, err := searchOutageThenSelect(t, down, healthy, ReplaceState{})

	if ports.IsSourceUnavailable(err) || willRetry(ctx, err) {
		t.Fatalf("err = %v, want a permanent failure", err)
	}
}

func TestSelectStep_HealthyNoCandidates_StaysNoMatchFound(t *testing.T) {
	healthy := findOnlySource{name: "healthy", candidates: []ports.AudioCandidate{rejectedCandidate}}

	ctx, err := searchOutageThenSelect(t, findOnlySource{name: "quiet"}, healthy, ReplaceState{})

	if got := err.Error(); got != "no candidates passed matching gates" {
		t.Errorf("error = %q", got)
	}
	wrapped := &StepError{Step: stepNameSelect, Err: err}
	if got := failureReason(wrapped); got != "no_match_found" || willRetry(ctx, wrapped) {
		t.Errorf("failureReason = %q, want permanent no_match_found", got)
	}
}

func TestSelectStep_OutageDoesNotLeakIntoNextAttempt(t *testing.T) {
	healthy := findOnlySource{name: "healthy", candidates: []ports.AudioCandidate{rejectedCandidate}}
	searchOutageThenSelect(t, outageSource(), healthy, ReplaceState{})

	ctx2 := withJobAttempt(context.Background(), 2)
	_, err := searchOutageThenSelect(t, findOnlySource{name: "quiet"}, healthy, ReplaceState{})

	if ports.IsSourceUnavailable(err) || willRetry(ctx2, err) {
		t.Fatalf("err = %v, want a permanent failure on attempt 2", err)
	}
}

func TestSelectStep_WrappedOutageOnFinalAttempt_DoesNotRetry(t *testing.T) {
	healthy := findOnlySource{name: "healthy", candidates: []ports.AudioCandidate{rejectedCandidate}}
	_, err := searchOutageThenSelect(t, outageSource(), healthy, ReplaceState{})

	if willRetry(withJobAttempt(context.Background(), maxAcquisitionAttempts), err) {
		t.Fatal("willRetry at the final attempt must be false")
	}
}
