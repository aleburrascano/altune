package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"testing"

	"github.com/google/uuid"
)

func seedTrackInRepo(t *testing.T, repo *fakeTrackRepository, userId shared.UserId, settle func(*domain.Track) error) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	if settle != nil {
		if err := settle(track); err != nil {
			t.Fatalf("settle track: %v", err)
		}
	}
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	return track
}

func storedTrack(t *testing.T, repo *fakeTrackRepository, track *domain.Track) *domain.Track {
	t.Helper()
	got := repo.tracks[track.ID.String()+":"+track.UserId.String()]
	if got == nil {
		t.Fatal("track missing from repository")
	}
	return got
}

func markReadyWith(ref string) func(*domain.Track) error {
	return func(tr *domain.Track) error { return tr.MarkReady(ref) }
}

// A failure reported by a job whose track a concurrent success already moved
// to ready must not clobber that good audio.
func TestAcquire_StaleFailureDoesNotOverwriteReadyTrack(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, markReadyWith("u/a/b/good.opus"))
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	svc.markFailed(context.Background(), track.ID, userId, "download_failed")

	got := storedTrack(t, repo, track)
	if got.AcquisitionStatus != domain.AcquisitionReady {
		t.Fatalf("status = %v, want ready: a stale failure overwrote a completed acquisition", got.AcquisitionStatus)
	}
	if got.AudioRef == nil || *got.AudioRef != "u/a/b/good.opus" {
		t.Errorf("AudioRef = %v, want the concurrent success's audio kept", got.AudioRef)
	}
}

// A duplicate failure must not overwrite the reason the first one recorded.
func TestAcquire_DuplicateFailureKeepsOriginalReason(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, func(tr *domain.Track) error {
		return tr.MarkFailed(string(domain.FailureAcquisitionInterrupted))
	})
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	svc.markFailed(context.Background(), track.ID, userId, "download_failed")

	got := storedTrack(t, repo, track)
	if got.FailureReason == nil || *got.FailureReason != string(domain.FailureAcquisitionInterrupted) {
		t.Errorf("FailureReason = %v, want the first failure's reason kept", got.FailureReason)
	}
}

func TestAcquire_FailureOnPendingTrackStillMarksFailed(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, nil)
	svc := NewAcquireTrackAudioService(repo, fakeRegistry(&fakeAudioSearcher{}), newFakeAudioStore())

	svc.markFailed(context.Background(), track.ID, userId, "download_failed")

	got := storedTrack(t, repo, track)
	if got.AcquisitionStatus != domain.AcquisitionFailed {
		t.Errorf("status = %v, want failed", got.AcquisitionStatus)
	}
}

// A second completion under the canonical key the first one already wrote
// rewrote that same object, so it must succeed: refusing it would roll back the
// store step and delete the audio the ready track serves.
func TestUpdateTrackStep_DuplicateCompletionUnderSameRefSucceeds(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, markReadyWith("u/a/b/song.opus"))
	step := NewUpdateTrackStep(repo, userId, track.ID)

	if _, err := step.Execute(context.Background(), &AcquisitionContext{AudioRef: "u/a/b/song.opus"}, afterStore{}); err != nil {
		t.Fatalf("duplicate completion under the same ref: %v", err)
	}
	got := storedTrack(t, repo, track)
	if got.AcquisitionStatus != domain.AcquisitionReady || got.AudioRef == nil || *got.AudioRef != "u/a/b/song.opus" {
		t.Errorf("track = %v %v, want ready with the shared ref", got.AcquisitionStatus, got.AudioRef)
	}
}

// A second completion under a different ref for an already-ready track (not a
// replace) is refused and keeps the audio the first completion stored.
func TestUpdateTrackStep_DuplicateCompletionUnderOtherRefIsRefused(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, markReadyWith("u/a/b/first.opus"))
	step := NewUpdateTrackStep(repo, userId, track.ID)

	_, err := step.Execute(context.Background(), &AcquisitionContext{AudioRef: "u/a/b/second.opus"}, afterStore{})

	if err == nil {
		t.Fatal("expected a duplicate completion to be refused")
	}
	got := storedTrack(t, repo, track)
	if got.AudioRef == nil || *got.AudioRef != "u/a/b/first.opus" {
		t.Errorf("AudioRef = %v, want the first completion's audio kept", got.AudioRef)
	}
}

func TestUpdateTrackStep_ReplaceSwapsReadyAudio(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, markReadyWith("u/a/b/old.opus"))
	step := NewUpdateTrackStep(repo, userId, track.ID)
	ac := &AcquisitionContext{AudioRef: "u/a/b/new.opus", Replace: ReplaceState{PreservedRef: "u/a/b/old.opus"}}

	if _, err := step.Execute(context.Background(), ac, afterStore{}); err != nil {
		t.Fatalf("replace update: %v", err)
	}
	got := storedTrack(t, repo, track)
	if got.AudioRef == nil || *got.AudioRef != "u/a/b/new.opus" {
		t.Errorf("AudioRef = %v, want the replacement audio", got.AudioRef)
	}
}

func TestUpdateTrackStep_RollbackOnPendingTrackIsANoOp(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	repo := newFakeTrackRepository()
	track := seedTrackInRepo(t, repo, userId, nil)
	marker := track.AcquisitionStartedAt
	step := NewUpdateTrackStep(repo, userId, track.ID)

	if err := step.Rollback(context.Background(), &AcquisitionContext{}); err != nil {
		t.Fatalf("rollback of a pending track: %v", err)
	}
	got := storedTrack(t, repo, track)
	if got.AcquisitionStartedAt != marker {
		t.Error("rollback refreshed the in-flight marker of a track that was never marked ready")
	}
}
