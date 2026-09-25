package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"context"
	"errors"
	"testing"
)

// TestAddTrackService_RefusedScheduleFailsTrack is the regression for a shed
// acquisition stranding a new track at pending: nothing would ever run its job,
// and the retry endpoint only admits failed tracks. A refused schedule must fail
// the track at once, persist it, and tell the client, so retry can reclaim it.
func TestAddTrackService_RefusedScheduleFailsTrack(t *testing.T) {
	ctx := context.Background()
	repo := catalogtest.NewTrackRepo()
	sched := &catalogtest.Scheduler{Err: errors.New("acquisition queue is full")}
	pub := &recordingPlaylistPublisher{}
	svc := NewAddTrackService(repo, WithAcquisitionScheduler(sched), WithAddTrackEvents(pub))

	out, err := svc.Execute(ctx, testUserId(), AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("Execute = %v, want the track created despite the refused schedule", err)
	}
	if len(sched.TrackIds) != 1 {
		t.Fatalf("schedule attempts = %d, want 1", len(sched.TrackIds))
	}

	stored, _ := repo.GetByID(ctx, out.Track.ID, out.Track.UserId)
	if stored == nil || stored.AcquisitionStatus != domain.AcquisitionFailed {
		t.Fatalf("stored track = %+v, want failed (not stranded pending)", stored)
	}
	if stored.FailureReason == nil || *stored.FailureReason != string(domain.FailureAcquisitionRefused) {
		t.Errorf("failure reason = %v, want %q", stored.FailureReason, domain.FailureAcquisitionRefused)
	}
	if stored.AcquisitionStartedAt != nil {
		t.Error("in-flight marker left set on a track whose job was never queued")
	}
	if got := pub.last("track_acquisition_failed"); got == nil || got["reason"] != string(domain.FailureAcquisitionRefused) {
		t.Errorf("track_acquisition_failed payload = %v, want reason %q", got, domain.FailureAcquisitionRefused)
	}
}

// If the failed state cannot be persisted, the response and events must not
// claim it: the stored row is still pending (the stale sweep reclaims it).
func TestAddTrackService_RefusedScheduleUnpersistedStaysPending(t *testing.T) {
	ctx := context.Background()
	repo := catalogtest.NewTrackRepo()
	repo.ErrOnUpdate = errors.New("db down")
	pub := &recordingPlaylistPublisher{}
	sched := &catalogtest.Scheduler{Err: errors.New("acquisition queue is full")}
	svc := NewAddTrackService(repo, WithAcquisitionScheduler(sched), WithAddTrackEvents(pub))

	out, err := svc.Execute(ctx, testUserId(), AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Track.AcquisitionStatus != domain.AcquisitionPending || out.Track.AcquisitionStartedAt == nil {
		t.Errorf("returned track = %v (started_at %v), want pending as stored", out.Track.AcquisitionStatus, out.Track.AcquisitionStartedAt)
	}
	if pub.last("track_acquisition_failed") != nil {
		t.Error("published track_acquisition_failed for a failure that was never persisted")
	}
}

func TestAddTrackService_AcceptedScheduleLeavesTrackPending(t *testing.T) {
	ctx := context.Background()
	repo := catalogtest.NewTrackRepo()
	pub := &recordingPlaylistPublisher{}
	svc := NewAddTrackService(repo, WithAcquisitionScheduler(&catalogtest.Scheduler{}), WithAddTrackEvents(pub))

	out, err := svc.Execute(ctx, testUserId(), AddTrackInput{Title: "Track", Artist: "Artist", Album: "Album"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Track.AcquisitionStatus != domain.AcquisitionPending {
		t.Errorf("status = %v, want pending while the queued job runs", out.Track.AcquisitionStatus)
	}
	if pub.last("track_acquisition_failed") != nil {
		t.Error("published track_acquisition_failed for an accepted schedule")
	}
}
