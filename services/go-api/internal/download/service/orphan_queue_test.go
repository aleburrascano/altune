package service_test

import (
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/uuid"
)

type recordingOrphanQueue struct {
	mu       sync.Mutex
	recorded []ports.OrphanedAudio
	err      error
}

func (q *recordingOrphanQueue) RecordOrphanedAudio(_ context.Context, o ports.OrphanedAudio) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if q.err != nil {
		return q.err
	}
	q.recorded = append(q.recorded, o)
	return nil
}

func (q *recordingOrphanQueue) entries() []ports.OrphanedAudio {
	q.mu.Lock()
	defer q.mu.Unlock()
	return append([]ports.OrphanedAudio(nil), q.recorded...)
}

func TestExecuteReplace_FailedOldAudioDeleteIsQueuedForTheSweep(t *testing.T) {
	track := seedReadyTrack(t)
	track.store.deleteErr = errors.New("object storage unavailable")
	queue := &recordingOrphanQueue{}
	grace := 24 * time.Hour
	svc := track.service(service.WithAcquireOrphanQueue(queue), service.WithSupersededAudioGrace(grace))

	before := time.Now()
	if err := svc.ExecuteReplace(context.Background(), track.userID, track.trackID); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	recorded := queue.entries()
	if len(recorded) != 1 {
		t.Fatalf("recorded = %+v, want exactly one orphan", recorded)
	}
	got := recorded[0]
	notBefore := got.NotBefore
	got.NotBefore = time.Time{}
	want := ports.OrphanedAudio{AudioRef: track.originalRef, UserId: track.userID, TrackId: track.trackID}
	if got != want {
		t.Errorf("recorded = %+v, want %+v", got, want)
	}
	if notBefore.Before(before.Add(grace)) || notBefore.After(time.Now().Add(grace)) {
		t.Errorf("not before = %s, want now plus %s", notBefore, grace)
	}
}

func TestExecuteReplace_SupersededAudioIsQueuedAfterTheGraceAndKept(t *testing.T) {
	track := seedReadyTrack(t)
	queue := &recordingOrphanQueue{}
	grace := 168 * time.Hour
	svc := track.service(service.WithAcquireOrphanQueue(queue), service.WithSupersededAudioGrace(grace))

	before := time.Now()
	if err := svc.ExecuteReplace(context.Background(), track.userID, track.trackID); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	recorded := queue.entries()
	if len(recorded) != 1 {
		t.Fatalf("recorded = %+v, want exactly one orphan", recorded)
	}
	got := recorded[0]
	if got.AudioRef != track.originalRef || got.UserId != track.userID || got.TrackId != track.trackID {
		t.Errorf("recorded = %+v, want the superseded ref %s for user %s track %s", got, track.originalRef, track.userID, track.trackID)
	}
	if got.NotBefore.Before(before.Add(grace)) || got.NotBefore.After(time.Now().Add(grace)) {
		t.Errorf("not before = %s, want now plus %s", got.NotBefore, grace)
	}
	if !track.store.holds(track.originalRef) {
		t.Error("the superseded object was deleted inside the grace window")
	}
}

func TestExecuteReplace_QueueFailureDoesNotFailTheJob(t *testing.T) {
	track := seedReadyTrack(t)
	track.store.deleteErr = errors.New("object storage unavailable")
	queue := &recordingOrphanQueue{err: errors.New("queue down")}
	svc := track.service(service.WithAcquireOrphanQueue(queue))

	if err := svc.ExecuteReplace(context.Background(), track.userID, track.trackID); err != nil {
		t.Fatalf("ExecuteReplace: %v, want success despite the queue failing", err)
	}
}

func TestExecuteReplace_NothingIsDeletedAtCommitAndExactlyOneEntryIsQueued(t *testing.T) {
	track := seedReadyTrack(t)
	queue := &recordingOrphanQueue{}
	svc := track.service(service.WithAcquireOrphanQueue(queue), service.WithSupersededAudioGrace(time.Hour))

	if err := svc.ExecuteReplace(context.Background(), track.userID, track.trackID); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}
	if !track.store.holds(track.originalRef) {
		t.Error("the superseded object was deleted at commit, want it kept for the sweep")
	}
	if recorded := queue.entries(); len(recorded) != 1 || recorded[0].AudioRef != track.originalRef {
		t.Errorf("recorded = %+v, want exactly one entry for %s", recorded, track.originalRef)
	}
}

func TestExecuteReplace_FailedRollbackDeleteIsQueuedForTheSweep(t *testing.T) {
	track := seedReadyTrack(t)
	track.ledger.FailWrites(errors.New("transient db error"))
	track.store.deleteErr = errors.New("object storage unavailable")
	queue := &recordingOrphanQueue{}
	svc := track.service(service.WithAcquireOrphanQueue(queue))

	if err := svc.ExecuteReplace(context.Background(), track.userID, track.trackID); err == nil {
		t.Fatal("expected the replace to fail")
	}

	recorded := queue.entries()
	if len(recorded) != 1 {
		t.Fatalf("recorded = %+v, want exactly one orphan", recorded)
	}
	got := recorded[0]
	if got.AudioRef == track.originalRef || got.AudioRef == "" || got.UserId != track.userID || got.TrackId != track.trackID {
		t.Errorf("recorded = %+v, want the staged ref for user %s track %s", got, track.userID, track.trackID)
	}
}

func TestStoreRollback_ExhaustedDeleteQueuesTheOrphan(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		userID, trackID := ids.NewUserId(uuid.New()), ids.NewTrackId()
		store := &objectStore{objects: map[string]string{"u/new.mp3": "new bytes"}, deleteErr: errors.New("down")}
		queue := &recordingOrphanQueue{}
		step := service.NewStoreStep(store, service.WithStoreOrphanQueue(queue, userID),
			service.WithStoreAudioRefGuard(acquisitiontest.NewLedger(), trackID))

		if err := step.Rollback(context.Background(), &service.AcquisitionContext{AudioRef: "u/new.mp3"}); err == nil {
			t.Fatal("want the delete failure surfaced")
		}

		want := ports.OrphanedAudio{AudioRef: "u/new.mp3", UserId: userID, TrackId: trackID}
		if recorded := queue.entries(); len(recorded) != 1 || recorded[0] != want {
			t.Errorf("recorded = %+v, want exactly %+v", recorded, want)
		}
	})
}

func TestStoreRollback_SharedRefIsNeverQueued(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		userID, own := ids.NewUserId(uuid.New()), ids.NewTrackId()
		sharedRef := "u/shared.mp3"
		refs := acquisitiontest.NewLedger(acquisitiontest.LedgerTrack{TrackSnapshot: ports.TrackSnapshot{
			ID: ids.NewTrackId(), UserId: userID, Status: ports.TrackReady, AudioRef: &sharedRef,
		}})
		queue := &recordingOrphanQueue{}
		store := &objectStore{objects: map[string]string{}, deleteErr: errors.New("down")}
		step := service.NewStoreStep(store, service.WithStoreOrphanQueue(queue, userID), service.WithStoreAudioRefGuard(refs, own))

		if err := step.Rollback(context.Background(), &service.AcquisitionContext{AudioRef: sharedRef}); err != nil {
			t.Fatalf("Rollback: %v", err)
		}
		if recorded := queue.entries(); len(recorded) != 0 {
			t.Errorf("recorded = %+v, want none", recorded)
		}
	})
}
