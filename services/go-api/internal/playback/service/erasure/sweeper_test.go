package erasure

import (
	"altune/go-api/internal/playback/playbacktest"
	"altune/go-api/internal/playback/ports"
	"altune/go-api/internal/playback/service/queue"
	"altune/go-api/internal/shared"
	"bytes"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

type identityStore struct {
	queue *playbacktest.InMemoryQueueRepo
	live  map[uuid.UUID]bool
	err   error
	scans []shared.UserId

	stallOnScan int
}

func newIdentityStore(queue *playbacktest.InMemoryQueueRepo) *identityStore {
	return &identityStore{queue: queue, live: map[uuid.UUID]bool{}}
}

func (s *identityStore) deleteAccount(userId shared.UserId) {
	delete(s.live, userId.UUID())
}

func (s *identityStore) ListOwnersWithoutIdentity(ctx context.Context, after shared.UserId, window int) (ports.OwnerScanPage, error) {
	s.scans = append(s.scans, after)
	if s.stallOnScan > 0 && len(s.scans) == s.stallOnScan {
		<-ctx.Done()
		return ports.OwnerScanPage{}, fmt.Errorf("list owners without identity: %w", ctx.Err())
	}
	if s.err != nil {
		return ports.OwnerScanPage{}, s.err
	}
	if window <= 0 {
		return ports.OwnerScanPage{}, fmt.Errorf("window must be positive, got %d", window)
	}
	var keys []uuid.UUID
	for id := range s.queue.States {
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

func saveQueueOf(t *testing.T, svc *queue.Service, identities *identityStore, sourceId string) shared.UserId {
	t.Helper()
	user := playbacktest.TestUser()
	if err := svc.Save(context.Background(), user, queue.SaveQueueStateInput{
		TrackIds:       []string{playbacktest.Trk("a"), playbacktest.Trk("b")},
		CurrentIdx:     1,
		RepeatMode:     "off",
		LegacySourceId: sourceId,
	}); err != nil {
		t.Fatalf("save: %v", err)
	}
	identities.live[user.UUID()] = true
	return user
}

func newSweep(repo *playbacktest.InMemoryQueueRepo, identities *identityStore) *Sweeper {
	return NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}))
}

func TestForgetDeletedIdentities_ErasesTheQueueStateOfADeletedAccount(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	svc := queue.New(repo, &playbacktest.FakeNowPlaying{})
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
	logs := playbacktest.CaptureLogs(t)
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	const privateSourceId = "search:mac demarco"
	deleted := saveQueueOf(t, queue.New(repo, &playbacktest.FakeNowPlaying{}), identities, privateSourceId)
	identities.deleteAccount(deleted)

	if _, err := newSweep(repo, identities).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec := playbacktest.AuditRecord(t, logs, "playback.queue_state_forgotten")
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
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	user := saveQueueOf(t, queue.New(repo, &playbacktest.FakeNowPlaying{}), identities, "search:pino daniele")
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
	repo := playbacktest.NewInMemoryQueueRepo()
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
	logs := playbacktest.CaptureLogs(t)
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	deleted := saveQueueOf(t, queue.New(repo, &playbacktest.FakeNowPlaying{}), identities, "search:mac demarco")
	identities.deleteAccount(deleted)
	dbDown := errors.New("connection refused")
	svc := NewSweeper(identities,
		queue.New(&playbacktest.UndeletableQueueRepo{Err: dbDown}, &playbacktest.FakeNowPlaying{}))

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
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	svc := queue.New(repo, &playbacktest.FakeNowPlaying{})
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
	if len(repo.States) != 0 {
		t.Errorf("%d deleted accounts still hold queue state after one run", len(repo.States))
	}
}

type failingOwnersQueueRepo struct {
	playbacktest.InMemoryQueueRepo
	failing  map[shared.UserId]error
	attempts map[shared.UserId]int
}

func (r *failingOwnersQueueRepo) DeleteForUser(ctx context.Context, userId shared.UserId) error {
	if r.attempts != nil {
		r.attempts[userId]++
	}
	if err := r.failing[userId]; err != nil {
		return err
	}
	return r.InMemoryQueueRepo.DeleteForUser(ctx, userId)
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
	repo := &failingOwnersQueueRepo{InMemoryQueueRepo: *playbacktest.NewInMemoryQueueRepo(), failing: map[shared.UserId]error{}}
	identities := newIdentityStore(&repo.InMemoryQueueRepo)
	svc := queue.New(repo, &playbacktest.FakeNowPlaying{})
	first := saveQueueOf(t, svc, identities, "search:a")
	failing := saveQueueOf(t, svc, identities, "search:b")
	last := saveQueueOf(t, svc, identities, "search:c")
	for _, u := range []shared.UserId{first, failing, last} {
		identities.deleteAccount(u)
	}
	dbDown := errors.New("connection refused")
	repo.failing[failing] = dbDown
	sweep := NewSweeper(identities, svc)

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

func TestForgetDeletedIdentities_EndlessScanStopsAtTheRunBudget(t *testing.T) {
	listing := &endlessListing{owner: shared.NewUserId(uuid.New())}
	sweep := NewSweeper(listing, queue.New(playbacktest.NewInMemoryQueueRepo(), &playbacktest.FakeNowPlaying{}),
		WithSweepRunBudget(20*time.Millisecond))

	started := time.Now()
	_, err := sweep.Execute(context.Background())
	if err != nil {
		t.Fatalf("Execute = %v, want a spent budget to end the run quietly", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Errorf("run took %s, want it bounded by its 20ms budget", elapsed)
	}
	if listing.calls < 2 {
		t.Errorf("listed %d pages, want the run to keep paging until its budget", listing.calls)
	}
}

func deletedOwnersInKeyOrder(t *testing.T, svc *queue.Service, identities *identityStore, n int) []shared.UserId {
	t.Helper()
	var owners []shared.UserId
	for range n {
		owner := saveQueueOf(t, svc, identities, "search:pino daniele")
		identities.deleteAccount(owner)
		owners = append(owners, owner)
	}
	slices.SortFunc(owners, func(a, b shared.UserId) int {
		ka, kb := a.UUID(), b.UUID()
		return bytes.Compare(ka[:], kb[:])
	})
	return owners
}

func runScans(sweep *Sweeper, identities *identityStore) (int, []shared.UserId, error) {
	identities.scans = nil
	forgotten, err := sweep.Execute(context.Background())
	return forgotten, identities.scans, err
}

func TestForgetDeletedIdentities_OneRunFinishesAMultiPageTable(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	owners := deletedOwnersInKeyOrder(t, queue.New(repo, &playbacktest.FakeNowPlaying{}), identities, 5)
	sweep := newSweep(repo, identities)
	sweep.scanWindow = 2

	forgotten, scans, err := runScans(sweep, identities)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if forgotten != 5 || len(repo.States) != 0 {
		t.Fatalf("one run forgot %d accounts and left %d, want all 5 erased in a single pass", forgotten, len(repo.States))
	}
	if want := []shared.UserId{{}, owners[1], owners[3]}; !slices.Equal(scans, want) {
		t.Errorf("run scanned after %v, want every page of the table %v", scans, want)
	}
	if _, scans, _ := runScans(sweep, identities); len(scans) == 0 || !scans[0].IsZero() {
		t.Errorf("the run after a finished pass scanned after %v, want it to start over", scans)
	}
}

func TestForgetDeletedIdentities_RunStopsAtItsDeadlineAndResumes(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	owners := deletedOwnersInKeyOrder(t, queue.New(repo, &playbacktest.FakeNowPlaying{}), identities, 5)
	sweep := NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}),
		WithSweepRunBudget(50*time.Millisecond))
	sweep.scanWindow = 2
	identities.stallOnScan = 3

	forgotten, _, err := runScans(sweep, identities)
	if err != nil {
		t.Fatalf("Execute(first run) = %v, want a spent budget to end the run quietly", err)
	}
	if forgotten != 4 {
		t.Fatalf("first run forgot %d accounts before its deadline, want the 4 on the two pages it finished", forgotten)
	}

	identities.stallOnScan = 0
	forgotten, scans, err := runScans(sweep, identities)
	if err != nil {
		t.Fatalf("Execute(second run): %v", err)
	}
	if !slices.Equal(scans, []shared.UserId{owners[3]}) || forgotten != 1 {
		t.Fatalf("second run scanned after %v and forgot %d, want it to resume after the fourth owner and erase the last one", scans, forgotten)
	}
}

func newFailingOwnersRepo() *failingOwnersQueueRepo {
	return &failingOwnersQueueRepo{InMemoryQueueRepo: *playbacktest.NewInMemoryQueueRepo(), failing: map[shared.UserId]error{}, attempts: map[shared.UserId]int{}}
}

func TestForgetDeletedIdentities_PermanentlyFailingOwnerDoesNotBlockTheTable(t *testing.T) {
	repo := newFailingOwnersRepo()
	identities := newIdentityStore(&repo.InMemoryQueueRepo)
	owners := deletedOwnersInKeyOrder(t, queue.New(repo, &playbacktest.FakeNowPlaying{}), identities, 5)
	sweep := NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}),
		WithSweepRunBudget(50*time.Millisecond))
	sweep.scanWindow = 2
	dbDown := errors.New("connection refused")
	repo.failing[owners[0]] = dbDown
	identities.stallOnScan = 2

	forgotten, _, err := runScans(sweep, identities)
	if !errors.Is(err, dbDown) || forgotten != 1 || repo.attempts[owners[0]] != 1 {
		t.Fatalf("first run = (%d, %v) with %d attempts on the failing owner, want 1 erased, the failure reported and 1 attempt",
			forgotten, err, repo.attempts[owners[0]])
	}

	identities.stallOnScan = 0
	forgotten, scans, err := runScans(sweep, identities)
	if !errors.Is(err, dbDown) {
		t.Fatalf("second run = %v, want the failing owner reported again", err)
	}
	if repo.attempts[owners[0]] != 2 {
		t.Errorf("failing owner attempted %d times after two runs, want it retried every run", repo.attempts[owners[0]])
	}
	if len(scans) == 0 || scans[0] != owners[1] {
		t.Fatalf("second run scanned after %v, want the cursor past the finished page, after owner 2", scans)
	}
	if forgotten != 3 || len(repo.States) != 1 {
		t.Fatalf("second run forgot %d and left %d, want the 3 owners after the failing one erased", forgotten, len(repo.States))
	}
	if !slices.Equal(sweep.retryOwners, []shared.UserId{owners[0]}) {
		t.Errorf("retry set = %v, want only the failing owner", sweep.retryOwners)
	}
}

func TestForgetDeletedIdentities_RetriedOwnerLeavesTheRetrySetOnceErased(t *testing.T) {
	repo := newFailingOwnersRepo()
	identities := newIdentityStore(&repo.InMemoryQueueRepo)
	owners := deletedOwnersInKeyOrder(t, queue.New(repo, &playbacktest.FakeNowPlaying{}), identities, 3)
	sweep := NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}))
	dbDown := errors.New("connection refused")
	repo.failing[owners[1]] = dbDown

	if forgotten, _, err := runScans(sweep, identities); !errors.Is(err, dbDown) || forgotten != 2 {
		t.Fatalf("first run = (%d, %v), want the 2 healthy owners erased and the failure reported", forgotten, err)
	}

	delete(repo.failing, owners[1])
	forgotten, _, err := runScans(sweep, identities)
	if err != nil || forgotten != 1 || len(repo.States) != 0 {
		t.Fatalf("second run = (%d, %v) leaving %d, want the failed owner erased on retry", forgotten, err, len(repo.States))
	}
	if len(sweep.retryOwners) != 0 {
		t.Errorf("retry set = %v after a successful retry, want it empty", sweep.retryOwners)
	}
}

func TestForgetDeletedIdentities_FullRetrySetDropsAndCountsTheOverflow(t *testing.T) {
	repo := newFailingOwnersRepo()
	identities := newIdentityStore(&repo.InMemoryQueueRepo)
	owners := deletedOwnersInKeyOrder(t, queue.New(repo, &playbacktest.FakeNowPlaying{}), identities, 3)
	spy := &sweepMetricsSpy{}
	sweep := NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}), WithErasureSweepMetrics(spy))
	sweep.retryCap = 2
	for _, owner := range owners {
		repo.failing[owner] = errors.New("connection refused")
	}

	if _, _, err := runScans(sweep, identities); err == nil {
		t.Fatal("Execute must report the failed erases")
	}

	if len(sweep.retryOwners) != 2 || spy.retryDropped != 1 {
		t.Errorf("retry set holds %d with %d drops counted, want it capped at 2 and 1 drop counted", len(sweep.retryOwners), spy.retryDropped)
	}
}

type sweepMetricsSpy struct {
	idle         int
	erased       int
	scanTimeouts int
	retryDropped int
}

func (m *sweepMetricsSpy) SweepIdle()             { m.idle++ }
func (m *sweepMetricsSpy) QueueStateErased(n int) { m.erased += n }
func (m *sweepMetricsSpy) OwnerScanTimedOut()     { m.scanTimeouts++ }
func (m *sweepMetricsSpy) RetryOwnerDropped()     { m.retryDropped++ }

func TestForgetDeletedIdentities_IdleSweepIsCounted(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("query: %w", ports.ErrIdentityStoreUnavailable)
	spy := &sweepMetricsSpy{}
	svc := NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}), WithErasureSweepMetrics(spy))

	if _, err := svc.Execute(context.Background()); !errors.Is(err, ports.ErrIdentityStoreUnavailable) {
		t.Fatalf("Execute = %v, want ErrIdentityStoreUnavailable", err)
	}

	if spy.idle != 1 || spy.erased != 0 {
		t.Errorf("idle=%d erased=%d, want 1 and 0", spy.idle, spy.erased)
	}
}

func TestForgetDeletedIdentities_ErasedAccountsAreCounted(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	user := saveQueueOf(t, queue.New(repo, &playbacktest.FakeNowPlaying{}), identities, "search:x")
	identities.deleteAccount(user)
	spy := &sweepMetricsSpy{}
	svc := NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}), WithErasureSweepMetrics(spy))

	if _, err := svc.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if spy.erased != 1 || spy.idle != 0 {
		t.Errorf("idle=%d erased=%d, want 0 and 1", spy.idle, spy.erased)
	}
}

func TestForgetDeletedIdentities_UnreadableIdentityStoreFailsTheRun(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("query: %w", ports.ErrIdentityStoreUnavailable)
	spy := &sweepMetricsSpy{}
	svc := NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}), WithErasureSweepMetrics(spy))

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
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	reaper := &reaperSpy{}
	svc := NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}), WithErasedStateReaper(reaper))

	if _, err := svc.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if reaper.calls != 1 {
		t.Errorf("reaper ran %d times, want 1 even with no orphaned owners", reaper.calls)
	}
}

func TestForgetDeletedIdentities_ReapsWhenIdentityStoreIsUnavailable(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("query: %w", ports.ErrIdentityStoreUnavailable)
	reaper := &reaperSpy{}
	svc := NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}), WithErasedStateReaper(reaper))

	if _, err := svc.Execute(context.Background()); !errors.Is(err, ports.ErrIdentityStoreUnavailable) {
		t.Fatalf("Execute = %v, want ErrIdentityStoreUnavailable", err)
	}

	if reaper.calls != 1 {
		t.Errorf("reaper ran %d times, want 1 while the identity store is down", reaper.calls)
	}
}

func TestForgetDeletedIdentities_ReapFailureDoesNotStopTheErasure(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	deleted := saveQueueOf(t, queue.New(repo, &playbacktest.FakeNowPlaying{}), identities, "search:x")
	identities.deleteAccount(deleted)
	dbDown := errors.New("connection refused")
	svc := NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}),
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
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("list owners without identity: %w", context.DeadlineExceeded)
	spy := &sweepMetricsSpy{}
	svc := NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}), WithErasureSweepMetrics(spy))

	_, err := svc.Execute(context.Background())

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute = %v, want the scan deadline reported", err)
	}
	if spy.scanTimeouts != 1 || spy.idle != 0 {
		t.Errorf("scanTimeouts=%d idle=%d, want 1 and 0", spy.scanTimeouts, spy.idle)
	}
}

func TestForgetDeletedIdentities_CancelledRunIsNotAScanTimeout(t *testing.T) {
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	identities.err = context.DeadlineExceeded
	spy := &sweepMetricsSpy{}
	svc := NewSweeper(identities, queue.New(repo, &playbacktest.FakeNowPlaying{}), WithErasureSweepMetrics(spy))
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
	logs := playbacktest.CaptureLogs(t)
	repo := playbacktest.NewInMemoryQueueRepo()
	identities := newIdentityStore(repo)
	const privateSourceId = "search:mac demarco"
	deleted := saveQueueOf(t, queue.New(repo, &playbacktest.FakeNowPlaying{}), identities, privateSourceId)
	identities.deleteAccount(deleted)

	if _, err := newSweep(repo, identities).Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	rec := playbacktest.AuditRecord(t, logs, "playback.queue_state_forgotten")
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
