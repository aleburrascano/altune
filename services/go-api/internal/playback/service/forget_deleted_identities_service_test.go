package service

import (
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type identityStore struct {
	queue *inMemoryQueueRepo
	live  map[uuid.UUID]bool
	err   error
	scans []shared.UserId
}

func newIdentityStore(queue *inMemoryQueueRepo) *identityStore {
	return &identityStore{queue: queue, live: map[uuid.UUID]bool{}}
}

func (s *identityStore) deleteAccount(userId shared.UserId) {
	delete(s.live, userId.UUID())
}

func (s *identityStore) ListOwnersWithoutIdentity(_ context.Context, after shared.UserId, window int) (ports.OwnerScanPage, error) {
	s.scans = append(s.scans, after)
	if s.err != nil {
		return ports.OwnerScanPage{}, s.err
	}
	if window <= 0 {
		return ports.OwnerScanPage{}, fmt.Errorf("window must be positive, got %d", window)
	}
	var keys []uuid.UUID
	for id := range s.queue.states {
		if bytes.Compare(id[:], afterKey(after)) > 0 {
			keys = append(keys, id)
		}
	}
	slices.SortFunc(keys, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	page := ports.OwnerScanPage{Next: after, Done: len(keys) < window}
	for _, id := range keys[:min(window, len(keys))] {
		page.Next = shared.NewUserId(id)
		if !s.live[id] {
			page.Orphaned = append(page.Orphaned, page.Next)
		}
	}
	return page, nil
}

func afterKey(after shared.UserId) []byte {
	key := after.UUID()
	return key[:]
}

func saveQueueOf(t *testing.T, svc *QueueService, identities *identityStore, sourceId string) shared.UserId {
	t.Helper()
	user := testUser()
	if err := svc.Save(context.Background(), user, SaveQueueStateInput{
		TrackIds:       []string{trk("a"), trk("b")},
		CurrentIdx:     1,
		RepeatMode:     "off",
		LegacySourceId: sourceId,
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
	for range ownerScanWindow + overflow {
		identities.deleteAccount(saveQueueOf(t, svc, identities, "search:pino daniele"))
	}

	forgotten, err := newSweep(repo, identities).Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if forgotten != ownerScanWindow+overflow {
		t.Errorf("run forgot %d accounts, want the whole backlog of %d", forgotten, ownerScanWindow+overflow)
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

type endlessListing struct {
	owner shared.UserId
	calls int
}

func (l *endlessListing) ListOwnersWithoutIdentity(_ context.Context, _ shared.UserId, _ int) (ports.OwnerScanPage, error) {
	l.calls++
	return ports.OwnerScanPage{Orphaned: []shared.UserId{l.owner}, Next: l.owner}, nil
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

func TestForgetDeletedIdentities_StopsAtThePageCapWhenTheScanNeverEnds(t *testing.T) {
	listing := &endlessListing{owner: shared.NewUserId(uuid.New())}
	sweep := NewForgetDeletedIdentitiesService(listing, NewQueueService(newInMemoryQueueRepo(), &fakeNowPlaying{}))

	forgotten, err := sweep.Execute(context.Background())

	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if listing.calls != maxOwnerScanPagesPerRun {
		t.Errorf("listed %d pages, want the cap of %d", listing.calls, maxOwnerScanPagesPerRun)
	}
	if forgotten != maxOwnerScanPagesPerRun {
		t.Errorf("forgot %d, want one per capped page (%d)", forgotten, maxOwnerScanPagesPerRun)
	}
}

func TestForgetDeletedIdentities_ResumesTheOwnerScanWhereTheLastRunStopped(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	svc := NewQueueService(repo, &fakeNowPlaying{})
	var owners []uuid.UUID
	for range 5 {
		owners = append(owners, saveQueueOf(t, svc, identities, "search:pino daniele").UUID())
	}
	slices.SortFunc(owners, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	sweep := newSweep(repo, identities)
	sweep.scanWindow, sweep.maxPages = 2, 2

	runScans := func(run string) []shared.UserId {
		t.Helper()
		identities.scans = nil
		if _, err := sweep.Execute(context.Background()); err != nil {
			t.Fatalf("Execute(%s): %v", run, err)
		}
		return identities.scans
	}

	if got := runScans("first"); !slices.Equal(got, []shared.UserId{{}, shared.NewUserId(owners[1])}) {
		t.Fatalf("first run scanned after %v, want the start then the second owner", got)
	}
	if got := runScans("second"); !slices.Equal(got, []shared.UserId{shared.NewUserId(owners[3])}) {
		t.Fatalf("second run scanned after %v, want it to resume after the fourth owner and finish the pass", got)
	}
	if got := runScans("third"); len(got) == 0 || !got[0].IsZero() {
		t.Fatalf("the run after a finished pass scanned after %v, want it to start over", got)
	}
}

type sweepMetricsSpy struct {
	idle         int
	erased       int
	scanTimeouts int
}

func (m *sweepMetricsSpy) SweepIdle()             { m.idle++ }
func (m *sweepMetricsSpy) QueueStateErased(n int) { m.erased += n }
func (m *sweepMetricsSpy) OwnerScanTimedOut()     { m.scanTimeouts++ }

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

func TestForgetDeletedIdentities_OwnerScanDeadlineIsCounted(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("list owners without identity: %w", context.DeadlineExceeded)
	spy := &sweepMetricsSpy{}
	svc := NewForgetDeletedIdentitiesService(identities, NewQueueService(repo, &fakeNowPlaying{}), WithErasureSweepMetrics(spy))

	_, err := svc.Execute(context.Background())

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute = %v, want the scan deadline reported", err)
	}
	if spy.scanTimeouts != 1 || spy.idle != 0 {
		t.Errorf("scanTimeouts=%d idle=%d, want 1 and 0", spy.scanTimeouts, spy.idle)
	}
}

func TestForgetDeletedIdentities_CancelledRunIsNotAScanTimeout(t *testing.T) {
	repo := newInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	identities.err = context.DeadlineExceeded
	spy := &sweepMetricsSpy{}
	svc := NewForgetDeletedIdentitiesService(identities, NewQueueService(repo, &fakeNowPlaying{}), WithErasureSweepMetrics(spy))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := svc.Execute(ctx); err == nil {
		t.Fatal("Execute on a cancelled run must report the failure")
	}
	if spy.scanTimeouts != 0 {
		t.Errorf("scanTimeouts=%d, want 0 when the run itself was cancelled", spy.scanTimeouts)
	}
}

func TestForgetDeletedIdentities_AuditRecordNamesTheSweepTrigger(t *testing.T) {
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
	if rec["trigger"] != "sweep" {
		t.Errorf("trigger = %v, want sweep", rec["trigger"])
	}
	if rec["action"] != "queue_state.forget" || rec["user_id"] != deleted.String() || rec["object"] != "playback_queue_state" {
		t.Errorf("record = %v, want action, user_id and object unchanged", rec)
	}
	if out := logs.String(); strings.Contains(out, privateSourceId) {
		t.Errorf("the audit trail leaks the PII it says was erased; logs=%s", out)
	}
}
