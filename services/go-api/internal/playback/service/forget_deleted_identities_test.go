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

// identityStore is the out-of-band Supabase identity table as this service can
// see it: which owners of stored queue state still have an account. Dropping an
// owner from it is an account deleted in Supabase, which tells this service
// nothing and leaves its queue-state row behind.
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

// saveQueueOf stores one user's queue state and registers their account as
// live, so a test starts from a user who exists and has something to erase.
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

// TestForgetDeletedIdentities_ErasesTheQueueStateOfADeletedAccount reproduces
// #1593: an identity deleted out-of-band in Supabase left its queue state (full
// track list, natural order and free-text search source_id, all PII) stored
// indefinitely, because nothing but the self-service DELETE route — which the
// deleted identity can no longer authenticate for — ever reached Forget.
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

// TestForgetDeletedIdentities_LeavesTheSelfServiceAuditRecord pins that the
// erasure #1567 audits is audited identically however it was triggered: the
// record says the PII is gone, so a sweep that erased without one would leave
// the deletion unprovable.
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

// TestForgetDeletedIdentities_UnreadableIdentityStoreErasesNothing pins the
// blast bound: where the identity store cannot be read (a plain Postgres with
// no Supabase auth schema, a role denied the table) every owner would look
// deleted, so the sweep must idle rather than erase the whole table.
func TestForgetDeletedIdentities_UnreadableIdentityStoreErasesNothing(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	user := saveQueueOf(t, NewQueueService(repo, &fakeNowPlaying{}), identities, "search:pino daniele")
	identities.err = fmt.Errorf("query: %w", ports.ErrIdentityStoreUnavailable)

	forgotten, err := newSweep(repo, identities).Execute(context.Background())
	if err != nil {
		t.Fatalf("an identity store this deployment cannot read is not a sweep failure: %v", err)
	}

	if forgotten != 0 {
		t.Errorf("forgot %d accounts against an unreadable identity store, want 0", forgotten)
	}
	if stored, _ := repo.GetForUser(context.Background(), user); stored == nil {
		t.Error("the sweep erased a live user's queue state on an unreadable identity store")
	}
}

// TestForgetDeletedIdentities_ListingFailureIsReported keeps a broken identity
// read from passing as a clean run: the job health is the only place an erasure
// that never happens can surface.
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

// TestForgetDeletedIdentities_FailedErasureStopsTheRun pins that a refused
// delete is retried rather than counted: the run reports the failure and the
// account stays in the next run's batch.
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

// TestForgetDeletedIdentities_ErasesABoundedBatchPerRun pins that a backlog of
// deleted accounts drains a batch at a time: one run must not hold the identity
// store open for an unbounded scan, and the accounts it left must be erased by
// the run after it.
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
// signal that a broken grant has stopped erasure.
func TestForgetDeletedIdentities_IdleSweepIsCounted(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("query: %w", ports.ErrIdentityStoreUnavailable)
	spy := &sweepMetricsSpy{}
	svc := NewForgetDeletedIdentitiesService(identities, NewQueueService(repo, &fakeNowPlaying{}), WithErasureSweepMetrics(spy))

	if _, err := svc.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
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
