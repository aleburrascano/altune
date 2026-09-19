package service

import (
	"context"
	"errors"
	"testing"
	"time"
)

// flakyDeleteStore is an AudioWriter whose Delete fails the first failDeletes
// calls, then succeeds. It lets a rollback test drive both the permanent and
// the transient delete-failure paths.
type flakyDeleteStore struct {
	stored      map[string]bool
	deleteErr   error
	failDeletes int
	deleteCalls int
}

func (s *flakyDeleteStore) Exists(_ context.Context, audioRef string) (bool, error) {
	return s.stored[audioRef], nil
}

func (s *flakyDeleteStore) Store(_ context.Context, _ string, audioRef string) error {
	s.stored[audioRef] = true
	return nil
}

func (s *flakyDeleteStore) Delete(_ context.Context, audioRef string) error {
	s.deleteCalls++
	if s.deleteCalls <= s.failDeletes {
		return s.deleteErr
	}
	delete(s.stored, audioRef)
	return nil
}

// A rollback whose delete never succeeds must not silently return nil: doing so
// orphans the stored object with no signal to the caller, no retry, and no
// reaper. The failure has to be surfaced (wrapped) so the pipeline sees it.
func TestStoreRollback_SurfacesDeleteFailureInsteadOfOrphaning(t *testing.T) {
	deleteErr := errors.New("object store unavailable")
	store := &flakyDeleteStore{
		stored:      map[string]bool{"u/a/b/new.mp3": true},
		deleteErr:   deleteErr,
		failDeletes: 1 << 30, // never succeeds
	}
	step := NewStoreStep(store)
	step.sleep = func(_ time.Duration) {}

	ac := &AcquisitionContext{AudioRef: "u/a/b/new.mp3"}
	err := step.Rollback(context.Background(), ac)
	if err == nil {
		t.Fatal("Rollback swallowed a permanent delete failure; the orphaned object is invisible to the caller")
	}
	if !errors.Is(err, deleteErr) {
		t.Fatalf("Rollback error must wrap the underlying delete failure, got %v", err)
	}
	if store.deleteCalls < 2 {
		t.Errorf("a retryable compensation must attempt the delete more than once, got %d", store.deleteCalls)
	}
}

// A transient delete failure must be retried until it succeeds so the object is
// actually removed rather than left orphaned.
func TestStoreRollback_RetriesTransientDeleteFailure(t *testing.T) {
	store := &flakyDeleteStore{
		stored:      map[string]bool{"u/a/b/new.mp3": true},
		deleteErr:   errors.New("transient blip"),
		failDeletes: 2,
	}
	step := NewStoreStep(store)
	step.sleep = func(_ time.Duration) {}

	ac := &AcquisitionContext{AudioRef: "u/a/b/new.mp3"}
	if err := step.Rollback(context.Background(), ac); err != nil {
		t.Fatalf("Rollback must retry a transient delete failure, got %v", err)
	}
	if store.stored["u/a/b/new.mp3"] {
		t.Error("object left orphaned: compensation did not delete it after retrying")
	}
	if store.deleteCalls < 3 {
		t.Errorf("expected the delete to be retried until success, got %d calls", store.deleteCalls)
	}
}

// cancelSensitiveStore refuses every call on a done context, the way a real
// object-store client does once its request context is cancelled.
type cancelSensitiveStore struct {
	stored map[string]bool
}

func (s *cancelSensitiveStore) Exists(ctx context.Context, audioRef string) (bool, error) {
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return s.stored[audioRef], nil
}

func (s *cancelSensitiveStore) Store(ctx context.Context, _ string, audioRef string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.stored[audioRef] = true
	return nil
}

func (s *cancelSensitiveStore) Delete(ctx context.Context, audioRef string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	delete(s.stored, audioRef)
	return nil
}

// The store stage completes, then the job is cancelled — an acquireTimeout or a
// scheduler shutdown. The compensating delete must still remove the object it
// wrote; nothing else ever will, since no reaper covers orphaned audio.
func TestStoreRollback_DeletesStoredAudioAfterTheJobContextIsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	store := &cancelSensitiveStore{stored: map[string]bool{}}
	storeStep := NewStoreStep(store)
	storeStep.sleep = func(_ time.Duration) {}

	updateTrack := newMockStep("update_track", nil)
	updateTrack.cancelJob = cancel
	updateTrack.executeErr = errors.New("scheduler shutting down")

	pipeline := pipelineOf()
	pipeline.store = storeStep
	pipeline.updateTrack = mockStage[afterStore, afterUpdate]{updateTrack}

	ac := &AcquisitionContext{
		Track:    TrackRef{UserID: "u1", Artist: "Daft Punk", Album: "Discovery", Title: "One More Time"},
		TempPath: "/tmp/one-more-time.mp3",
	}

	if err := RunPipeline(ctx, pipeline, ac); err == nil {
		t.Fatal("expected the cancelled pipeline to fail, got nil")
	}

	if store.stored[ac.AudioRef] {
		t.Fatalf("audio %q left orphaned: the rollback delete ran on the cancelled job context", ac.AudioRef)
	}
}

// Two textually-equivalent-but-differently-cased/Unicode-composed artist names
// must resolve to the same physical storage path, so one artist is never split
// across multiple folders on the case-sensitive Linux target.
func TestBuildAudioRefNormalizesCaseAndUnicode(t *testing.T) {
	cases := []struct {
		name string
		a    TrackRef
		b    TrackRef
	}{
		{
			name: "case fold",
			a:    TrackRef{UserID: "u1", Artist: "Daft Punk", Album: "Discovery", Title: "One More Time"},
			b:    TrackRef{UserID: "u1", Artist: "DAFT PUNK", Album: "DISCOVERY", Title: "ONE MORE TIME"},
		},
		{
			name: "unicode composition (precomposed vs decomposed é)",
			a:    TrackRef{UserID: "u1", Artist: "Beyoncé", Album: "Lemonade", Title: "Sorry"},
			b:    TrackRef{UserID: "u1", Artist: "Beyoncé", Album: "Lemonade", Title: "Sorry"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildAudioRef(tc.a, "track.mp3")
			want := BuildAudioRef(tc.b, "track.mp3")
			if got != want {
				t.Fatalf("storage paths diverge for equivalent metadata:\n a = %q\n b = %q", got, want)
			}
		})
	}
}
