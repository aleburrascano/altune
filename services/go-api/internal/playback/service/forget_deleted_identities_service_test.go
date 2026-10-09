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

func TestForgetDeletedIdentities_ErasesABacklogAcrossBatchesInOneRun(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	svc := NewQueueService(repo, &fakeNowPlaying{})
	const overflow = 3
	for range deletedIdentityBatch + overflow {
		identities.deleteAccount(saveQueueOf(t, svc, identities, "search:pino daniele"))
	}

	forgotten, err := newSweep(repo, identities).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if forgotten != deletedIdentityBatch+overflow {
		t.Errorf("run forgot %d accounts, want the whole backlog of %d", forgotten, deletedIdentityBatch+overflow)
	}
	if len(repo.states) != 0 {
		t.Errorf("%d deleted accounts still hold queue state after one run", len(repo.states))
	}
}

type failingOwnersQueueRepo struct {
	inMemoryQueueRepo
	failing map[shared.UserId]error
}

func (r *failingOwnersQueueRepo) DeleteForUser(ctx context.Context, userId shared.UserId) error {
	if err := r.failing[userId]; err != nil {
		return err
	}
	return r.inMemoryQueueRepo.DeleteForUser(ctx, userId)
}

type fixedListing struct {
	owners []shared.UserId
	calls  int
}

func (l *fixedListing) ListOwnersWithoutIdentity(_ context.Context, _ int) ([]shared.UserId, error) {
	l.calls++
	return l.owners, nil
}

func TestForgetDeletedIdentities_OneFailingOwnerDoesNotBlockTheRest(t *testing.T) {
	repo := &failingOwnersQueueRepo{inMemoryQueueRepo: *newInMemoryQueueRepo(), failing: map[shared.UserId]error{}}
	identities := newIdentityStore(&repo.inMemoryQueueRepo)
	svc := NewQueueService(repo, &fakeNowPlaying{})
	first := saveQueueOf(t, svc, identities, "search:a")
	failing := saveQueueOf(t, svc, identities, "search:b")
	last := saveQueueOf(t, svc, identities, "search:c")
	for _, u := range []shared.UserId{first, failing, last} {
		identities.deleteAccount(u)
	}
	dbDown := errors.New("connection refused")
	repo.failing[failing] = dbDown
	sweep := NewForgetDeletedIdentitiesService(identities, svc)

	forgotten, err := sweep.Execute(context.Background())

	if !errors.Is(err, dbDown) {
		t.Fatalf("Execute = %v, want an error wrapping the failing owner", err)
	}
	if forgotten != 2 {
		t.Errorf("forgot %d accounts, want the 2 healthy ones", forgotten)
	}
	if stored, _ := repo.GetForUser(context.Background(), failing); stored == nil {
		t.Error("the failing owner's state vanished without an erasure")
	}
	for _, u := range []shared.UserId{first, last} {
		if stored, _ := repo.GetForUser(context.Background(), u); stored != nil {
			t.Errorf("owner %s behind or after the failing one was not erased", u)
		}
	}
}

func TestForgetDeletedIdentities_StopsAtTheCapWhenOwnersKeepFailing(t *testing.T) {
	dbDown := errors.New("connection refused")
	owners := make([]shared.UserId, deletedIdentityBatch)
	failing := map[shared.UserId]error{}
	for i := range owners {
		owners[i] = shared.NewUserId(uuid.New())
		if i > 0 {
			failing[owners[i]] = dbDown
		}
	}
	listing := &fixedListing{owners: owners}
	repo := &failingOwnersQueueRepo{inMemoryQueueRepo: *newInMemoryQueueRepo(), failing: failing}
	sweep := NewForgetDeletedIdentitiesService(listing, NewQueueService(repo, &fakeNowPlaying{}))

	forgotten, err := sweep.Execute(context.Background())

	if !errors.Is(err, dbDown) {
		t.Fatalf("Execute = %v, want the failures reported", err)
	}
	if listing.calls != maxDeletedIdentityBatchesPerRun {
		t.Errorf("listed %d batches, want the cap of %d", listing.calls, maxDeletedIdentityBatchesPerRun)
	}
	if forgotten != maxDeletedIdentityBatchesPerRun {
		t.Errorf("forgot %d, want one per capped batch (%d)", forgotten, maxDeletedIdentityBatchesPerRun)
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

type reaperSpy struct {
	calls int
	err   error
}

func (r *reaperSpy) ReapErasedStates(_ context.Context) (int64, error) {
	r.calls++
	return 0, r.err
}

func TestForgetDeletedIdentities_ReapsWhenNoOwnerIsOrphaned(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	reaper := &reaperSpy{}
	svc := NewForgetDeletedIdentitiesService(identities, NewQueueService(repo, &fakeNowPlaying{}), WithErasedStateReaper(reaper))

	if _, err := svc.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if reaper.calls != 1 {
		t.Errorf("reaper ran %d times, want 1 even with no orphaned owners", reaper.calls)
	}
}

func TestForgetDeletedIdentities_ReapsWhenIdentityStoreIsUnavailable(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("query: %w", ports.ErrIdentityStoreUnavailable)
	reaper := &reaperSpy{}
	svc := NewForgetDeletedIdentitiesService(identities, NewQueueService(repo, &fakeNowPlaying{}), WithErasedStateReaper(reaper))

	if _, err := svc.Execute(context.Background()); !errors.Is(err, ports.ErrIdentityStoreUnavailable) {
		t.Fatalf("Execute = %v, want ErrIdentityStoreUnavailable", err)
	}

	if reaper.calls != 1 {
		t.Errorf("reaper ran %d times, want 1 while the identity store is down", reaper.calls)
	}
}

func TestForgetDeletedIdentities_ReapFailureDoesNotStopTheErasure(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	deleted := saveQueueOf(t, NewQueueService(repo, &fakeNowPlaying{}), identities, "search:x")
	identities.deleteAccount(deleted)
	dbDown := errors.New("connection refused")
	svc := NewForgetDeletedIdentitiesService(identities, NewQueueService(repo, &fakeNowPlaying{}),
		WithErasedStateReaper(&reaperSpy{err: dbDown}))

	forgotten, err := svc.Execute(context.Background())

	if forgotten != 1 {
		t.Errorf("forgot %d accounts after a reap failure, want 1", forgotten)
	}
	if !errors.Is(err, dbDown) {
		t.Errorf("Execute = %v, want the reap failure reported", err)
	}
}
