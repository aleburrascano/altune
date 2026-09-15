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

// cancellingWriter ends the job context mid-upload, then fails with a
// transport error that does not wrap ctx.Err().
type cancellingWriter struct {
	cancel func()
}

func (w *cancellingWriter) Exists(context.Context, string) (bool, error) { return false, nil }
func (w *cancellingWriter) Delete(context.Context, string) error         { return nil }
func (w *cancellingWriter) Store(context.Context, string, string) error {
	w.cancel()
	return errors.New("put object: connection reset")
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

func TestStoreStep_CancelledMidStore_ReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	step := NewStoreStep(&cancellingWriter{cancel: cancel})
	ac := &AcquisitionContext{Track: TrackRef{UserID: "u1", Title: "Song", Artist: "Artist"}, TempPath: "/tmp/x/track.mp3"}

	_, err := step.Execute(ctx, ac, afterTag{})
	if err == nil {
		t.Fatal("expected an error from a cancelled store")
	}
	if got := failureReason(&StepError{Step: "store", Err: err}); got != string(domain.FailureAcquisitionCancelled) {
		t.Errorf("failureReason = %q, want %q", got, domain.FailureAcquisitionCancelled)
	}
}

// cancellingSource cancels the job mid-search through the real SourceRegistry.
type cancellingSource struct {
	*fakeAudioSearcher
	cancel func()
}

func (s *cancellingSource) Find(_ context.Context, _ ports.FindRequest) ([]ports.AudioCandidate, error) {
	s.cancel()
	return nil, errors.New("upstream closed")
}

// End to end: the persisted failure_reason of a job cancelled mid-search is
// the cancellation code, not no_match_found.
func TestExecute_CancelledMidSearch_PersistsCancellation(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	repo := newFakeTrackRepository()
	key := track.ID.String() + ":" + userId.String()
	repo.tracks[key] = track
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	source := &cancellingSource{fakeAudioSearcher: &fakeAudioSearcher{}, cancel: cancel}
	svc := NewAcquireTrackAudioService(repo, NewSourceRegistry(source), newFakeAudioStore())

	_ = svc.Execute(ctx, userId, track.ID)

	if got := deref(repo.tracks[key].FailureReason); got != string(domain.FailureAcquisitionCancelled) {
		t.Errorf("persisted failure_reason = %q, want %q", got, domain.FailureAcquisitionCancelled)
	}
}

// A genuine failure with a live context keeps its permanent reason.
func TestSearchAndStoreSteps_GenuineFailure_KeepsStepReason(t *testing.T) {
	ctx := context.Background()

	_, searchErr := NewSearchStep(&cancellingFinder{cancel: func() {}}).
		Execute(ctx, &AcquisitionContext{Track: TrackRef{Title: "Song", Artist: "Artist"}}, pipelineStart{})
	if got := failureReason(&StepError{Step: "search", Err: searchErr}); got != string(domain.FailureNoMatchFound) {
		t.Errorf("search failureReason = %q, want %q", got, domain.FailureNoMatchFound)
	}

	ac := &AcquisitionContext{Track: TrackRef{UserID: "u1", Title: "Song", Artist: "Artist"}, TempPath: "/tmp/x/track.mp3"}
	_, storeErr := NewStoreStep(&cancellingWriter{cancel: func() {}}).Execute(ctx, ac, afterTag{})
	if got := failureReason(&StepError{Step: "store", Err: storeErr}); got != string(domain.FailureStorageFailed) {
		t.Errorf("store failureReason = %q, want %q", got, domain.FailureStorageFailed)
	}
}

// failureCode must classify a wrapped context error as cancellation for every
// step, not only via the pipeline's "pipeline cancelled" prefix.
func TestFailureReason_WrappedContextErrorIsCancellationForEveryStep(t *testing.T) {
	for _, step := range []string{"search", "select", "download", "tag", "store", "update_track"} {
		for _, ctxErr := range []error{context.Canceled, context.DeadlineExceeded} {
			err := &StepError{Step: step, Err: errors.Join(errors.New("adapter failed"), ctxErr)}
			if got := failureReason(err); got != string(domain.FailureAcquisitionCancelled) {
				t.Errorf("failureReason(%s, %v) = %q, want %q", step, ctxErr, got, domain.FailureAcquisitionCancelled)
			}
		}
	}
}
