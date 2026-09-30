package service

import (
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type identityStore struct {
	queue *inMemoryQueueRepo
	live  map[uuid.UUID]bool
	err   error
}

func newIdentityStore(queue *inMemoryQueueRepo) *identityStore {
	return &identityStore{queue: queue, live: map[uuid.UUID]bool{}}
}

func (s *identityStore) deleteAccount(userId shared.UserId) {
	delete(s.live, userId.UUID())
}

func (s *identityStore) ListOwnersWithoutIdentity(_ context.Context, limit int) ([]shared.UserId, error) {
	if s.err != nil {
		return nil, s.err
	}
	if limit <= 0 {
		return nil, fmt.Errorf("limit must be positive, got %d", limit)
	}
	var owners []shared.UserId
	for id := range s.queue.states {
		if len(owners) == limit {
			break
		}
		if !s.live[id] {
			owners = append(owners, shared.NewUserId(id))
		}
	}
	return owners, nil
}

func saveQueueOf(t *testing.T, svc *QueueService, identities *identityStore, sourceId string) shared.UserId {
	t.Helper()
	user := testUser()
	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:   []string{"a", "b"},
		CurrentIdx: 1,
		RepeatMode: "off",
		SourceId:   sourceId,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	identities.live[user.UUID()] = true
	return user
}

func newSweep(repo *inMemoryQueueRepo, identities *identityStore) *ForgetDeletedIdentitiesService {
	return NewForgetDeletedIdentitiesService(identities, NewQueueService(repo, &fakeNowPlaying{}))
}

func TestForgetDeletedIdentities_ErasesTheQueueStateOfADeletedAccount(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	svc := NewQueueService(repo, &fakeNowPlaying{})
	deleted := saveQueueOf(t, svc, identities, "search:mac demarco")
	stillRegistered := saveQueueOf(t, svc, identities, "search:pino daniele")
	identities.deleteAccount(deleted)

	forgotten, err := newSweep(repo, identities).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if forgotten != 1 {
		t.Errorf("forgot %d accounts, want 1", forgotten)
	}
	if stored, _ := repo.GetForUser(context.Background(), deleted); stored != nil {
		t.Errorf("queue state of a deleted account survived the sweep: %+v", stored)
	}
	if stored, _ := repo.GetForUser(context.Background(), stillRegistered); stored == nil {
		t.Error("the sweep erased a user whose account still exists")
	}
}

func TestForgetDeletedIdentities_LeavesTheSelfServiceAuditRecord(t *testing.T) {
	logs := captureLogs(t)
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	const privateSourceId = "search:mac demarco"
	deleted := saveQueueOf(t, NewQueueService(repo, &fakeNowPlaying{}), identities, privateSourceId)
	identities.deleteAccount(deleted)

	if _, err := newSweep(repo, identities).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec := auditRecord(t, logs, "playback.queue_state_forgotten")
	if rec["action"] != "queue_state.forget" {
		t.Errorf("action = %v, want queue_state.forget", rec["action"])
	}
	if rec["user_id"] != deleted.String() {
		t.Errorf("user_id = %v, want the erased account %q", rec["user_id"], deleted.String())
	}
	if rec["object"] != "playback_queue_state" {
		t.Errorf("object = %v, want playback_queue_state", rec["object"])
	}
	if out := logs.String(); strings.Contains(out, privateSourceId) {
		t.Errorf("the audit trail leaks the PII it says was erased; logs=%s", out)
	}
}

func TestForgetDeletedIdentities_UnreadableIdentityStoreErasesNothing(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	user := saveQueueOf(t, NewQueueService(repo, &fakeNowPlaying{}), identities, "search:pino daniele")
	identities.err = fmt.Errorf("query: %w", ports.ErrIdentityStoreUnavailable)

	forgotten, err := newSweep(repo, identities).Execute(context.Background())
	if !errors.Is(err, ports.ErrIdentityStoreUnavailable) {
		t.Fatalf("Execute = %v, want ErrIdentityStoreUnavailable", err)
	}

	if forgotten != 0 {
		t.Errorf("forgot %d accounts against an unreadable identity store, want 0", forgotten)
	}
	if stored, _ := repo.GetForUser(context.Background(), user); stored == nil {
		t.Error("the sweep erased a live user's queue state on an unreadable identity store")
	}
}

func TestForgetDeletedIdentities_ListingFailureIsReported(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	dbDown := errors.New("connection refused")
	identities.err = dbDown

	forgotten, err := newSweep(repo, identities).Execute(context.Background())

	if !errors.Is(err, dbDown) {
		t.Fatalf("Execute = %v, want the listing failure propagated", err)
	}
	if forgotten != 0 {
		t.Errorf("forgot %d accounts on a failed listing, want 0", forgotten)
	}
}

func TestForgetDeletedIdentities_FailedErasureStopsTheRun(t *testing.T) {
	logs := captureLogs(t)
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	deleted := saveQueueOf(t, NewQueueService(repo, &fakeNowPlaying{}), identities, "search:mac demarco")
	identities.deleteAccount(deleted)
	dbDown := errors.New("connection refused")
	svc := NewForgetDeletedIdentitiesService(identities,
		NewQueueService(&undeletableQueueRepo{err: dbDown}, &fakeNowPlaying{}))

	forgotten, err := svc.Execute(context.Background())

	if !errors.Is(err, dbDown) {
		t.Fatalf("Execute = %v, want the refused erasure propagated", err)
	}
	if forgotten != 0 {
		t.Errorf("counted %d erasures that did not happen, want 0", forgotten)
	}
	if out := logs.String(); strings.Contains(out, "playback.queue_state_forgotten") {
		t.Errorf("an erasure that never happened was audited as done; logs=%s", out)
	}
}

func TestForgetDeletedIdentities_ErasesABoundedBatchPerRun(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	svc := NewQueueService(repo, &fakeNowPlaying{})
	const overflow = 3
	for range deletedIdentityBatch + overflow {
		identities.deleteAccount(saveQueueOf(t, svc, identities, "search:pino daniele"))
	}
	sweep := newSweep(repo, identities)

	firstRun, err := sweep.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute (first run): %v", err)
	}
	secondRun, err := sweep.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute (second run): %v", err)
	}

	if firstRun != deletedIdentityBatch {
		t.Errorf("first run forgot %d accounts, want a bounded %d", firstRun, deletedIdentityBatch)
	}
	if secondRun != overflow {
		t.Errorf("second run forgot %d accounts, want the %d the first run left", secondRun, overflow)
	}
	if len(repo.states) != 0 {
		t.Errorf("%d deleted accounts still hold queue state after both runs", len(repo.states))
	}
}

type sweepMetricsSpy struct {
	idle   int
	erased int
}

func (m *sweepMetricsSpy) SweepIdle()             { m.idle++ }
func (m *sweepMetricsSpy) QueueStateErased(n int) { m.erased += n }

func TestForgetDeletedIdentities_IdleSweepIsCounted(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("query: %w", ports.ErrIdentityStoreUnavailable)
	spy := &sweepMetricsSpy{}
	svc := NewForgetDeletedIdentitiesService(identities, NewQueueService(repo, &fakeNowPlaying{}), WithErasureSweepMetrics(spy))

	if _, err := svc.Execute(context.Background()); !errors.Is(err, ports.ErrIdentityStoreUnavailable) {
		t.Fatalf("Execute = %v, want ErrIdentityStoreUnavailable", err)
	}

	if spy.idle != 1 || spy.erased != 0 {
		t.Errorf("idle=%d erased=%d, want 1 and 0", spy.idle, spy.erased)
	}
}

func TestForgetDeletedIdentities_ErasedAccountsAreCounted(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	user := saveQueueOf(t, NewQueueService(repo, &fakeNowPlaying{}), identities, "search:x")
	identities.deleteAccount(user)
	spy := &sweepMetricsSpy{}
	svc := NewForgetDeletedIdentitiesService(identities, NewQueueService(repo, &fakeNowPlaying{}), WithErasureSweepMetrics(spy))

	if _, err := svc.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if spy.erased != 1 || spy.idle != 0 {
		t.Errorf("idle=%d erased=%d, want 0 and 1", spy.idle, spy.erased)
	}
}

func TestForgetDeletedIdentities_UnreadableIdentityStoreFailsTheRun(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("query: %w", ports.ErrIdentityStoreUnavailable)
	spy := &sweepMetricsSpy{}
	svc := NewForgetDeletedIdentitiesService(identities, NewQueueService(repo, &fakeNowPlaying{}), WithErasureSweepMetrics(spy))

	forgotten, err := svc.Execute(context.Background())

	if !errors.Is(err, ports.ErrIdentityStoreUnavailable) {
		t.Fatalf("Execute = %v, want ErrIdentityStoreUnavailable", err)
	}
	if forgotten != 0 || spy.idle != 1 {
		t.Errorf("forgotten=%d idle=%d, want 0 and 1", forgotten, spy.idle)
	}
}
