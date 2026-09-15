package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

// Issue #979: the acquisition side must persist a failure_reason whose code the
// catalog failure-message table recognises, so the wire failure_message is the
// specific one for the case instead of the generic fallback.
func TestExecute_FailureReasonResolvesToSpecificMessage(t *testing.T) {
	tests := []struct {
		name       string
		candidates []ports.AudioCandidate
	}{
		{"no candidates at all", nil},
		{"every candidate rejected (reason carries a summary)", []ports.AudioCandidate{{
			Title:   "Completely Unrelated Cooking Show Full Episode Forty Seven Nonsense",
			URL:     "yt:cooking",
			Channel: "CookingChannel",
		}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			userId := shared.NewUserId(uuid.New())
			track, err := domain.NewTrack(userId, "I've Been in Love Before", "Cutting Crew", "Broadcast")
			if err != nil {
				t.Fatalf("new track: %v", err)
			}
			repo := newFakeTrackRepository()
			repo.tracks[track.ID.String()+":"+userId.String()] = track
			searcher := &fakeAudioSearcher{searchResults: tt.candidates}
			svc := NewAcquireTrackAudioService(repo, fakeRegistry(searcher), newFakeAudioStore())

			_ = svc.Execute(context.Background(), userId, track.ID)

			updated := repo.tracks[track.ID.String()+":"+userId.String()]
			if updated == nil || updated.AcquisitionStatus != domain.AcquisitionFailed {
				t.Fatalf("track not marked failed: %+v", updated)
				return
			}
			const want = "Couldn't find this track"
			if got := domain.FailureMessage(updated.FailureReason); got != want {
				t.Errorf("FailureMessage(%q) = %q, want %q", deref(updated.FailureReason), got, want)
			}
		})
	}
}

// Every failure_reason code the acquisition side can emit must be a key of the
// catalog failure-message table; a code missing there silently degrades to the
// generic message.
func TestFailureReason_EveryCodeIsKnownToCatalog(t *testing.T) {
	errs := []error{
		errors.New("pipeline cancelled: context canceled"),
		errors.New("unexpected"),
	}
	for _, step := range []string{"search", "select", "download", "tag", "store", "update_track", "unknown"} {
		errs = append(errs, &StepError{Step: step, Err: errors.New("boom")})
	}
	for _, err := range errs {
		code := failureCode(err)
		if !code.Known() {
			t.Errorf("failureReason(%q) = %q, not a catalog failure code", err, code)
		}
		reason := string(code) + domain.FailureDetailSeparator + "all 1 candidate rejected (1 identity)"
		if got, want := domain.FailureMessage(&reason), domain.FailureMessage(new(string(code))); got != want {
			t.Errorf("summary suffix changed message for %q: %q, want %q", code, got, want)
		}
	}
}
