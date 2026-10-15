package service

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSelectStep_Execute(t *testing.T) {
	step := NewSelectStep()
	ac := &AcquisitionContext{
		Track: TrackRef{
			Title:           "Blinding Lights",
			Artist:          "The Weeknd",
			CatalogDuration: 200,
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
		Track:      TrackRef{Title: "Song", Artist: "Artist", CatalogDuration: 200},
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
		Track: TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", CatalogDuration: 200},
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
		Track:   TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", CatalogDuration: 200},
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
	return findOnlySource{name: "down", err: &acqdomain.SourceUnavailableError{Source: "down", Err: errors.New("429")}}
}

func TestSelectStep_SearchOutageWithAllRejected_IsRetryable(t *testing.T) {
	healthy := findOnlySource{name: "healthy", candidates: []ports.AudioCandidate{rejectedCandidate}}

	ctx, err := searchOutageThenSelect(t, outageSource(), healthy, ReplaceState{})

	if !acqdomain.IsSourceUnavailable(err) || !willRetry(ctx, err) {
		t.Fatalf("err = %v, want a retryable source-unavailable failure", err)
	}
}

func TestSearchStep_OutageWithOnlyStoredFileMatch_IsSourceUnavailable(t *testing.T) {
	healthy := findOnlySource{name: "healthy", candidates: []ports.AudioCandidate{matchingCandidate}}
	ctx := withJobAttempt(context.Background(), 1)
	ac := &AcquisitionContext{
		Track:   TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", CatalogDuration: 200},
		Replace: ReplaceState{ExcludeKeys: []string{sourceKey(matchingCandidate.URL)}},
	}

	_, err := NewSearchStep(NewSourceRegistry(outageSource(), healthy)).Execute(ctx, ac, pipelineStart{})

	if !acqdomain.IsSourceUnavailable(err) {
		t.Fatalf("err = %v, want source-unavailable", err)
	}
}

func TestSelectStep_PlainSourceErrorWithAllRejected_StaysPermanent(t *testing.T) {
	down := findOnlySource{name: "down", err: errors.New("exit 1")}
	healthy := findOnlySource{name: "healthy", candidates: []ports.AudioCandidate{rejectedCandidate}}

	ctx, err := searchOutageThenSelect(t, down, healthy, ReplaceState{})

	if acqdomain.IsSourceUnavailable(err) || willRetry(ctx, err) {
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

	if acqdomain.IsSourceUnavailable(err) || willRetry(ctx2, err) {
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

const (
	finesseRemixURL = "https://music.youtube.com/watch?v=remix"
	finesseAlbumURL = "https://music.youtube.com/watch?v=album"
)

func finesseRemix() ports.AudioCandidate {
	return ports.AudioCandidate{
		Title: "Finesse (Remix)", URL: finesseRemixURL, Channel: "YouTube Music Art Track", Duration: 218,
		Artists: []string{"Bruno Mars", "Cardi B"}, Album: "Finesse (Remix)", Kind: ports.KindArtTrack,
	}
}

func finesseAlbumTrack() ports.AudioCandidate {
	return ports.AudioCandidate{
		Title: "Finesse", URL: finesseAlbumURL, Channel: "YouTube Music Art Track", Duration: 191,
		Artists: []string{"Bruno Mars"}, Album: "24K Magic", Kind: ports.KindArtTrack,
	}
}

func finesseTrack() TrackRef {
	return TrackRef{ID: "track-1", Title: "Finesse", Artist: "Bruno Mars", Album: "24K Magic", CatalogDuration: 191}
}

func shadowLine(t *testing.T, logs string) map[string]any {
	t.Helper()
	for _, raw := range strings.Split(logs, "\n") {
		var line map[string]any
		if json.Unmarshal([]byte(raw), &line) == nil && line["msg"] == "acquisition.judge_shadow" {
			return line
		}
	}
	t.Fatalf("no acquisition.judge_shadow line in %s", logs)
	return nil
}

func TestSelectStep_ShadowLogsDisagreementWithoutChangingThePick(t *testing.T) {
	logs := captureJSONLogSync(t)
	ac := &AcquisitionContext{Track: finesseTrack(), Candidates: []ports.AudioCandidate{finesseRemix(), finesseAlbumTrack()}}
	ac.Track.CatalogDuration = 223

	if _, err := NewSelectStep().Execute(context.Background(), ac, afterSearch{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	line := shadowLine(t, logs.String())
	if line["agree"] != false || line["judge_url"] != "" || line["pipeline_url"] != ac.Selected.URL || ac.Selected.URL != finesseRemixURL {
		t.Fatalf("shadow line = %v, selected = %s, want the pipeline on the remix and the judge accepting neither", line, ac.Selected.URL)
	}
}

func TestSelectStep_ShadowLogsAgreement(t *testing.T) {
	logs := captureJSONLogSync(t)
	ac := &AcquisitionContext{Track: finesseTrack(), Candidates: []ports.AudioCandidate{finesseAlbumTrack()}}

	if _, err := NewSelectStep().Execute(context.Background(), ac, afterSearch{}); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	line := shadowLine(t, logs.String())
	if line["agree"] != true || ac.Selected.URL != finesseAlbumURL {
		t.Fatalf("shadow line = %v, selected = %s, want agreement on the album track", line, ac.Selected.URL)
	}
}

func TestSearchStep_FirstTierHoldingOnlyTheStoredFileStillOpensTheFallbackTier(t *testing.T) {
	storedArtTrack := ports.AudioCandidate{
		Title: "Phone (Interlude)", URL: "https://music.youtube.com/watch?v=TB0z2HVFxEE",
		Duration: 112, Kind: ports.KindArtTrack, Artists: []string{"SLAYR"},
	}
	first := findOnlySource{name: "ytmsearch", candidates: []ports.AudioCandidate{storedArtTrack}}
	registry := NewSourceRegistry(first).WithFallback(findOnlySource{name: "ytdlp"})
	ac := &AcquisitionContext{
		Track:   TrackRef{Title: "Phone (Interlude)", Artist: "SLAYR", CatalogDuration: 128},
		Replace: ReplaceState{ExcludeKeys: []string{sourceKey(storedArtTrack.URL)}},
	}

	_, err := NewSearchStep(registry).Execute(context.Background(), ac, pipelineStart{})

	if err == nil || !ac.FallbackSkipped {
		t.Fatalf("err = %v, FallbackSkipped = %v, want a failed first tier that opens the fallback", err, ac.FallbackSkipped)
	}
}
