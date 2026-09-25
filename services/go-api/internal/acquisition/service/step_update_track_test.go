package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"math"
	"testing"

	"github.com/google/uuid"
)

func TestUpdateTrackStep_Execute(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("failed to create track: %v", err)
	}

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	step := NewUpdateTrackStep(repo, userId, track.ID)
	ac := &AcquisitionContext{
		AudioRef: "user/artist/album/song.mp3",
	}

	_, execErr := step.Execute(context.Background(), ac, afterStore{})

	if execErr != nil {
		t.Fatalf("expected no error, got %v", execErr)
	}

	updated := repo.tracks[track.ID.String()+":"+userId.String()]
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("track status = %v, want %v", updated.AcquisitionStatus, domain.AcquisitionReady)
	}
	if updated.AudioRef == nil || *updated.AudioRef != "user/artist/album/song.mp3" {
		t.Errorf("track AudioRef = %v, want %q", updated.AudioRef, "user/artist/album/song.mp3")
	}
}

func TestUpdateTrackStep_Execute_InfiniteProbeLeavesDurationUnknown(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("failed to create track: %v", err)
	}
	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track
	step := NewUpdateTrackStep(repo, userId, track.ID)
	ac := &AcquisitionContext{AudioRef: "user/artist/album/song.mp3", ProbedDuration: math.Inf(1)}

	if _, execErr := step.Execute(context.Background(), ac, afterStore{}); execErr != nil {
		t.Fatalf("expected no error, got %v", execErr)
	}

	updated, ok := repo.tracks[track.ID.String()+":"+userId.String()]
	if !ok || updated == nil {
		t.Fatal("track missing after update")
	}
	if updated.AcquisitionStatus != domain.AcquisitionReady {
		t.Errorf("track status = %v, want %v", updated.AcquisitionStatus, domain.AcquisitionReady)
	}
	if updated.DurationSeconds != nil {
		t.Errorf("DurationSeconds = %v, want unset", *updated.DurationSeconds)
	}
}

func TestUpdateTrackStep_Execute_TrackNotFound(t *testing.T) {
	repo := newFakeTrackRepository()
	userId := shared.NewUserId(uuid.New())
	trackId := domain.NewTrackId()

	step := NewUpdateTrackStep(repo, userId, trackId)
	ac := &AcquisitionContext{
		AudioRef: "some/audio/ref.mp3",
	}

	_, err := step.Execute(context.Background(), ac, afterStore{})

	if err == nil {
		t.Fatal("expected error when track not found, got nil")
	}
	if got := err.Error(); got != "track not found for update" {
		t.Errorf("error = %q, want %q", got, "track not found for update")
	}
}

func TestUpdateTrackStep_Execute_EmptyAudioRef(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, _ := domain.NewTrack(userId, "Song", "Artist", "Album")

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	step := NewUpdateTrackStep(repo, userId, track.ID)
	ac := &AcquisitionContext{
		AudioRef: "",
	}

	_, err := step.Execute(context.Background(), ac, afterStore{})

	if err == nil {
		t.Fatal("expected error when audioRef is empty, got nil")
	}
}

func TestUpdateTrackStep_Rollback_RevertsToPending(t *testing.T) {
	userId := shared.NewUserId(uuid.New())
	track, err := domain.NewTrack(userId, "Song", "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	audioRef := "user/artist/album/song.mp3"
	_ = track.MarkReady(audioRef)

	repo := newFakeTrackRepository()
	repo.tracks[track.ID.String()+":"+userId.String()] = track

	step := NewUpdateTrackStep(repo, userId, track.ID)
	ac := &AcquisitionContext{}

	if err := step.Rollback(context.Background(), ac); err != nil {
		t.Fatalf("expected no error on rollback, got %v", err)
	}
	reverted := repo.tracks[track.ID.String()+":"+userId.String()]
	if reverted.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("track status after rollback = %v, want %v", reverted.AcquisitionStatus, domain.AcquisitionPending)
	}
}

func TestUpdateTrackStep_Name(t *testing.T) {
	step := NewUpdateTrackStep(nil, shared.UserId{}, domain.TrackId{})
	if got := step.Name(); got != "update_track" {
		t.Errorf("Name() = %q, want %q", got, "update_track")
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
