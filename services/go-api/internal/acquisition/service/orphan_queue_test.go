package service

import (
	"altune/go-api/internal/acquisition/adapters/catalogbridge"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type recordingOrphanQueue struct {
	recorded []ports.OrphanedAudio
	err      error
}

func (q *recordingOrphanQueue) RecordOrphanedAudio(_ context.Context, o ports.OrphanedAudio) error {
	if q.err != nil {
		return q.err
	}
	q.recorded = append(q.recorded, o)
	return nil
}

func TestExecuteReplace_FailedOldAudioDeleteIsQueuedForTheSweep(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	store.deleteErr = errors.New("object storage unavailable")
	queue := &recordingOrphanQueue{}
	grace := 24 * time.Hour
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store,
		WithAcquireOrphanQueue(queue), WithSupersededAudioGrace(grace))

	before := time.Now()
	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	if len(queue.recorded) != 1 {
		t.Fatalf("recorded = %+v, want exactly one orphan", queue.recorded)
	}
	got := queue.recorded[0]
	notBefore := got.NotBefore
	got.NotBefore = time.Time{}
	want := ports.OrphanedAudio{AudioRef: originalRef, UserId: userId, TrackId: trackId}
	if got != want {
		t.Errorf("recorded = %+v, want %+v", got, want)
	}
	if notBefore.Before(before.Add(grace)) || notBefore.After(time.Now().Add(grace)) {
		t.Errorf("not before = %s, want now plus %s", notBefore, grace)
	}
}

func TestExecuteReplace_SupersededAudioIsQueuedAfterTheGraceAndKept(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	queue := &recordingOrphanQueue{}
	grace := 168 * time.Hour
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store,
		WithAcquireOrphanQueue(queue), WithSupersededAudioGrace(grace))

	before := time.Now()
	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}

	if len(queue.recorded) != 1 {
		t.Fatalf("recorded = %+v, want exactly one orphan", queue.recorded)
	}
	got := queue.recorded[0]
	if got.AudioRef != originalRef || got.UserId != userId || got.TrackId != trackId {
		t.Errorf("recorded = %+v, want the superseded ref %s for user %s track %s", got, originalRef, userId, trackId)
	}
	if got.NotBefore.Before(before.Add(grace)) || got.NotBefore.After(time.Now().Add(grace)) {
		t.Errorf("not before = %s, want now plus %s", got.NotBefore, grace)
	}
	if _, kept := store.objects[originalRef]; !kept {
		t.Error("the superseded object was deleted inside the grace window")
	}
}

func TestExecuteReplace_QueueFailureDoesNotFailTheJob(t *testing.T) {
	repo, store, userId, trackId, _ := seedReadyTrack(t)
	store.deleteErr = errors.New("object storage unavailable")
	queue := &recordingOrphanQueue{err: errors.New("queue down")}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store, WithAcquireOrphanQueue(queue))

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v, want success despite the queue failing", err)
	}
}

func TestExecuteReplace_NothingIsDeletedAtCommitAndExactlyOneEntryIsQueued(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	queue := &recordingOrphanQueue{}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store,
		WithAcquireOrphanQueue(queue), WithSupersededAudioGrace(time.Hour))

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err != nil {
		t.Fatalf("ExecuteReplace: %v", err)
	}
	if _, kept := store.objects[originalRef]; !kept {
		t.Error("the superseded object was deleted at commit, want it kept for the sweep")
	}
	if len(queue.recorded) != 1 || queue.recorded[0].AudioRef != originalRef {
		t.Errorf("recorded = %+v, want exactly one entry for %s", queue.recorded, originalRef)
	}
}

func TestExecuteReplace_FailedRollbackDeleteIsQueuedForTheSweep(t *testing.T) {
	repo, store, userId, trackId, originalRef := seedReadyTrack(t)
	repo.updateErr = errors.New("transient db error")
	store.deleteErr = errors.New("object storage unavailable")
	queue := &recordingOrphanQueue{}
	svc := newTestAcquireService(catalogbridge.NewTrackLedger(repo), NewSourceRegistry(newAudioSource{}), store, WithAcquireOrphanQueue(queue))

	if err := svc.ExecuteReplace(context.Background(), userId, trackId); err == nil {
		t.Fatal("expected the replace to fail")
	}

	if len(queue.recorded) != 1 {
		t.Fatalf("recorded = %+v, want exactly one orphan", queue.recorded)
	}
	got := queue.recorded[0]
	if got.AudioRef == originalRef || got.AudioRef == "" || got.UserId != userId || got.TrackId != trackId {
		t.Errorf("recorded = %+v, want the staged ref for user %s track %s", got, userId, trackId)
	}
}

func TestStoreRollback_ExhaustedDeleteQueuesTheOrphan(t *testing.T) {
	userId, trackId := shared.NewUserId(uuid.New()), domain.NewTrackId()
	store := &flakyDeleteStore{stored: map[string]bool{"u/new.mp3": true}, deleteErr: errors.New("down"), failDeletes: 1 << 30}
	queue := &recordingOrphanQueue{}
	step := NewStoreStep(store, WithStoreOrphanQueue(queue, userId), WithStoreAudioRefGuard(trackRefIndex{}, trackId))
	step.sleep = func(time.Duration) {}

	if err := step.Rollback(context.Background(), &AcquisitionContext{AudioRef: "u/new.mp3"}); err == nil {
		t.Fatal("want the delete failure surfaced")
	}

	want := ports.OrphanedAudio{AudioRef: "u/new.mp3", UserId: userId, TrackId: trackId}
	if len(queue.recorded) != 1 || queue.recorded[0] != want {
		t.Errorf("recorded = %+v, want exactly %+v", queue.recorded, want)
	}
}

func TestStoreRollback_SharedOrPreservedRefIsNeverQueued(t *testing.T) {
	userId, own, other := shared.NewUserId(uuid.New()), domain.NewTrackId(), domain.NewTrackId()
	refs := trackRefIndex{holders: map[string][]domain.TrackId{"u/shared.mp3": {other}}}
	for name, ac := range map[string]*AcquisitionContext{
		"shared":    {AudioRef: "u/shared.mp3"},
		"preserved": {AudioRef: "u/kept.mp3", Replace: ReplaceState{PreservedRef: "u/kept.mp3"}},
	} {
		queue := &recordingOrphanQueue{}
		store := &flakyDeleteStore{stored: map[string]bool{}, deleteErr: errors.New("down"), failDeletes: 1 << 30}
		step := NewStoreStep(store, WithStoreOrphanQueue(queue, userId), WithStoreAudioRefGuard(refs, own))
		step.sleep = func(time.Duration) {}

		if err := step.Rollback(context.Background(), ac); err != nil {
			t.Fatalf("%s: Rollback: %v", name, err)
		}
		if len(queue.recorded) != 0 {
			t.Errorf("%s: recorded = %+v, want none", name, queue.recorded)
		}
	}
}
