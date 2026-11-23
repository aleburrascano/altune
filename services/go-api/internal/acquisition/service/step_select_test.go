package service_test

import (
	acqdomain "altune/go-api/internal/acquisition/internal/core/domain"
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/acquisition/sourcekey"
	shared "altune/go-api/internal/shared/kernel"
	"context"
	"errors"
	"testing"
)

const noCandidatePassed = "no candidates passed matching gates"

func blindingLightsRef() service.TrackRef {
	return service.TrackRef{Title: "Blinding Lights", Artist: "The Weeknd", CatalogDuration: 200}
}

func TestSelectStep_Execute(t *testing.T) {
	source := &searchSource{name: "ytdlp", found: []ports.AudioCandidate{{
		Title: "Blinding Lights", Channel: "The Weeknd - Topic", Duration: 200,
		URL: "https://youtube.com/watch?v=topic1", Categories: []string{"Music"}, ViewCount: 10_000_000,
	}}}
	ac := &service.AcquisitionContext{Track: blindingLightsRef()}

	err := runPipeline(context.Background(), service.NewSourceRegistry(source), ac)

	if step := failedStep(t, err).Step; step == "search" || step == "select" {
		t.Fatalf("err = %v, want the pipeline past select", err)
	}
	if ac.Selected == nil || ac.Selected.URL != "https://youtube.com/watch?v=topic1" {
		t.Errorf("Selected = %+v, want %q", ac.Selected, "https://youtube.com/watch?v=topic1")
	}
}

func TestSelectStep_Execute_AllCandidatesBelowThreshold(t *testing.T) {
	source := &searchSource{name: "ytdlp", found: []ports.AudioCandidate{{
		Title: "Cooking Tutorial Episode 47", Channel: "CookingChannel", Duration: 200,
		URL: "https://youtube.com/watch?v=cook1", Categories: []string{"Howto & Style"}, ViewCount: 50_000,
	}}}

	err := runPipeline(context.Background(), service.NewSourceRegistry(source), &service.AcquisitionContext{Track: blindingLightsRef()})

	step := failedStep(t, err)
	if step.Step != "select" || step.Err.Error() != noCandidatePassed {
		t.Errorf("err = %v, want select failing with %q", err, noCandidatePassed)
	}
}

func TestSelectStep_Name(t *testing.T) {
	if got := service.NewSelectStep().Name(); got != "select" {
		t.Errorf("Name() = %q, want %q", got, "select")
	}
}

var (
	rejectedCandidate = ports.AudioCandidate{Title: "Cooking Tutorial Episode 47", Channel: "CookingChannel", Duration: 20, URL: "https://example.com/cook"}
	matchingCandidate = ports.AudioCandidate{Title: "Blinding Lights", Channel: "The Weeknd - Topic", Duration: 200, URL: "https://example.com/match"}
)

func searchOutageThenSelect(t *testing.T, down, healthy *searchSource) error {
	t.Helper()
	err := runPipeline(context.Background(), service.NewSourceRegistry(down, healthy), &service.AcquisitionContext{Track: blindingLightsRef()})
	if step := failedStep(t, err).Step; step != "select" {
		t.Fatalf("err = %v, want a select error", err)
	}
	return err
}

func healthyButRejected() *searchSource {
	return &searchSource{name: "healthy", found: []ports.AudioCandidate{rejectedCandidate}}
}

func TestSelectStep_SearchOutageWithAllRejected_IsTransient(t *testing.T) {
	err := searchOutageThenSelect(t, throttled(), healthyButRejected())

	if !acqdomain.IsSourceUnavailable(err) || service.FailureCodeOf(err) != string(shared.FailureSourceUnavailable) {
		t.Fatalf("err = %v, want a transient source-unavailable failure", err)
	}
}

func TestSearchStep_OutageWithOnlyStoredFileMatch_IsSourceUnavailable(t *testing.T) {
	healthy := &searchSource{name: "healthy", found: []ports.AudioCandidate{matchingCandidate}}
	ac := &service.AcquisitionContext{
		Track:   blindingLightsRef(),
		Replace: service.ReplaceState{ExcludeKeys: []string{sourcekey.SourceKey(matchingCandidate.URL)}},
	}

	err := runPipeline(context.Background(), service.NewSourceRegistry(throttled(), healthy), ac)

	if failedStep(t, err).Step != "search" || !acqdomain.IsSourceUnavailable(err) {
		t.Fatalf("err = %v, want search failing as source-unavailable", err)
	}
}

func TestSelectStep_PlainSourceErrorWithAllRejected_StaysPermanent(t *testing.T) {
	err := searchOutageThenSelect(t, &searchSource{name: "down", err: errors.New("exit 1")}, healthyButRejected())

	if acqdomain.IsSourceUnavailable(err) || service.FailureCodeOf(err) != string(shared.FailureNoMatchFound) {
		t.Fatalf("err = %v, want a permanent failure", err)
	}
}

func TestSelectStep_HealthyNoCandidates_StaysNoMatchFound(t *testing.T) {
	err := searchOutageThenSelect(t, &searchSource{name: "quiet"}, healthyButRejected())

	if got := failedStep(t, err).Err.Error(); got != noCandidatePassed {
		t.Errorf("error = %q", got)
	}
	if got := service.FailureCodeOf(err); got != "no_match_found" || acqdomain.IsSourceUnavailable(err) {
		t.Errorf("failure code = %q, want permanent no_match_found", got)
	}
}

func TestSelectStep_OutageDoesNotLeakIntoTheNextRun(t *testing.T) {
	searchOutageThenSelect(t, throttled(), healthyButRejected())

	err := searchOutageThenSelect(t, &searchSource{name: "quiet"}, healthyButRejected())

	if acqdomain.IsSourceUnavailable(err) {
		t.Fatalf("err = %v, want a permanent failure on the next run", err)
	}
}

func TestSearchStep_FirstTierHoldingOnlyTheStoredFileStillOpensTheFallbackTier(t *testing.T) {
	storedArtTrack := ports.AudioCandidate{
		Title: "Phone (Interlude)", URL: "https://music.youtube.com/watch?v=TB0z2HVFxEE",
		Duration: 112, Kind: ports.KindArtTrack, Artists: []string{"SLAYR"},
	}
	first := &searchSource{name: "ytmsearch", found: []ports.AudioCandidate{storedArtTrack}}
	fallback := &searchSource{name: "ytdlp"}
	ac := &service.AcquisitionContext{
		Track:   service.TrackRef{Title: "Phone (Interlude)", Artist: "SLAYR", CatalogDuration: 128},
		Replace: service.ReplaceState{ExcludeKeys: []string{sourcekey.SourceKey(storedArtTrack.URL)}},
	}

	_ = runPipeline(context.Background(), service.NewSourceRegistry(first).WithFallback(fallback), ac)

	seen := fallback.seen()
	if len(seen) != 1 || !seen[0].FallbackOnly {
		t.Fatalf("fallback requests = %+v, want one fallback-only search after the first tier held only the stored file", seen)
	}
}
