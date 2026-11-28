package service_test

import (
	"altune/go-api/internal/playback/internal/adapters/memory"
	"altune/go-api/internal/playback/internal/domain"
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/playback/internal/service"
	"altune/go-api/internal/shared/ids"
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

const (
	ownerScanWindow = 500
	maxRetryOwners  = 1000

	stallingRunBudget = time.Second
)

type silentCleaner struct{}

func (silentCleaner) ReapErasedStates(context.Context) (int64, error) { return 0, nil }

type silentSweepMetrics struct{}

func (silentSweepMetrics) SweepIdle()           {}
func (silentSweepMetrics) QueueStateErased(int) {}
func (silentSweepMetrics) OwnerScanTimedOut()   {}
func (silentSweepMetrics) RetryOwnerDropped()   {}

type identityStore struct {
	queue *memory.QueueStateRepo
	live  map[uuid.UUID]bool
	err   error
	scans []ids.UserId

	stallOnScan int
}

func newIdentityStore(queue *memory.QueueStateRepo) *identityStore {
	return &identityStore{queue: queue, live: map[uuid.UUID]bool{}}
}

func (s *identityStore) deleteAccount(userId ids.UserId) {
	delete(s.live, userId.UUID())
}

func (s *identityStore) ListOwnersWithoutIdentity(ctx context.Context, after ids.UserId, window int) (ports.OwnerScanPage, error) {
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
	for _, owner := range s.queue.UserIds() {
		id := owner.UUID()
		if bytes.Compare(id[:], afterKey(after)) > 0 {
			keys = append(keys, id)
		}
	}
	slices.SortFunc(keys, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	page := ports.OwnerScanPage{Next: after, Done: len(keys) < window}
	for _, id := range keys[:min(window, len(keys))] {
		page.Next = ids.NewUserId(id)
		if !s.live[id] {
			page.Orphaned = append(page.Orphaned, page.Next)
		}
	}
	return page, nil
}

func afterKey(after ids.UserId) []byte {
	key := after.UUID()
	return key[:]
}

func saveQueueOf(t *testing.T, svc *service.QueueService, identities *identityStore, sourceId string) ids.UserId {
	t.Helper()
	user := testUser()
	if err := svc.Save(context.Background(), user, service.SaveQueueStateInput{
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

func newSweep(repo *memory.QueueStateRepo, identities *identityStore) *service.ErasureSweeper {
	return service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), silentSweepMetrics{}, silentCleaner{}, service.DefaultSweepRunBudget)
}

func TestForgetDeletedIdentities_ErasesTheQueueStateOfADeletedAccount(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	svc := service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now)
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
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	const privateSourceId = "search:mac demarco"
	deleted := saveQueueOf(t, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), identities, privateSourceId)
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
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	user := saveQueueOf(t, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), identities, "search:pino daniele")
	identities.err = fmt.Errorf("query: %w", domain.ErrIdentityStoreUnavailable)

	forgotten, err := newSweep(repo, identities).Execute(context.Background())
	if !errors.Is(err, domain.ErrIdentityStoreUnavailable) {
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
	repo := memory.NewQueueStateRepo()
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
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	deleted := saveQueueOf(t, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), identities, "search:mac demarco")
	identities.deleteAccount(deleted)
	dbDown := errors.New("connection refused")
	svc := service.NewErasureSweeper(identities, service.MustNewQueueService(&undeletableQueueRepo{Err: dbDown}, &fakeNowPlaying{}, time.Now), silentSweepMetrics{}, silentCleaner{}, service.DefaultSweepRunBudget)

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
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	svc := service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now)
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
	if repo.Len() != 0 {
		t.Errorf("%d deleted accounts still hold queue state after one run", repo.Len())
	}
}

type failingOwnersQueueRepo struct {
	*memory.QueueStateRepo
	failing  map[ids.UserId]error
	attempts map[ids.UserId]int
}

func (r *failingOwnersQueueRepo) DeleteForUser(ctx context.Context, userId ids.UserId) error {
	if r.attempts != nil {
		r.attempts[userId]++
	}
	if err := r.failing[userId]; err != nil {
		return err
	}
	return r.QueueStateRepo.DeleteForUser(ctx, userId)
}

type endlessListing struct {
	owner ids.UserId
	calls int
}

func (l *endlessListing) ListOwnersWithoutIdentity(_ context.Context, _ ids.UserId, _ int) (ports.OwnerScanPage, error) {
	l.calls++
	return ports.OwnerScanPage{Orphaned: []ids.UserId{l.owner}, Next: l.owner}, nil
}

func TestForgetDeletedIdentities_OneFailingOwnerDoesNotBlockTheRest(t *testing.T) {
	repo := &failingOwnersQueueRepo{QueueStateRepo: memory.NewQueueStateRepo(), failing: map[ids.UserId]error{}}
	identities := newIdentityStore(repo.QueueStateRepo)
	svc := service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now)
	first := saveQueueOf(t, svc, identities, "search:a")
	failing := saveQueueOf(t, svc, identities, "search:b")
	last := saveQueueOf(t, svc, identities, "search:c")
	for _, u := range []ids.UserId{first, failing, last} {
		identities.deleteAccount(u)
	}
	dbDown := errors.New("connection refused")
	repo.failing[failing] = dbDown
	sweep := service.NewErasureSweeper(identities, svc, silentSweepMetrics{}, silentCleaner{}, service.DefaultSweepRunBudget)

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
	for _, u := range []ids.UserId{first, last} {
		if stored, _ := repo.GetForUser(context.Background(), u); stored != nil {
			t.Errorf("owner %s behind or after the failing one was not erased", u)
		}
	}
}

func TestForgetDeletedIdentities_EndlessScanStopsAtTheRunBudget(t *testing.T) {
	listing := &endlessListing{owner: ids.NewUserId(uuid.New())}
	sweep := service.NewErasureSweeper(listing, service.MustNewQueueService(memory.NewQueueStateRepo(), &fakeNowPlaying{}, time.Now), silentSweepMetrics{}, silentCleaner{}, 20*time.Millisecond)

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

func deletedOwnersInKeyOrder(t *testing.T, svc *service.QueueService, identities *identityStore, n int) []ids.UserId {
	t.Helper()
	var owners []ids.UserId
	for range n {
		owner := saveQueueOf(t, svc, identities, "search:pino daniele")
		identities.deleteAccount(owner)
		owners = append(owners, owner)
	}
	slices.SortFunc(owners, func(a, b ids.UserId) int {
		ka, kb := a.UUID(), b.UUID()
		return bytes.Compare(ka[:], kb[:])
	})
	return owners
}

func runScans(sweep *service.ErasureSweeper, identities *identityStore) (int, []ids.UserId, error) {
	identities.scans = nil
	forgotten, err := sweep.Execute(context.Background())
	return forgotten, identities.scans, err
}

func TestForgetDeletedIdentities_OneRunFinishesAMultiPageTable(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	owners := deletedOwnersInKeyOrder(t, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), identities, 2*ownerScanWindow+1)
	sweep := newSweep(repo, identities)

	forgotten, scans, err := runScans(sweep, identities)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if forgotten != len(owners) || repo.Len() != 0 {
		t.Fatalf("one run forgot %d accounts and left %d, want all %d erased in a single pass", forgotten, repo.Len(), len(owners))
	}
	if want := []ids.UserId{{}, owners[ownerScanWindow-1], owners[2*ownerScanWindow-1]}; !slices.Equal(scans, want) {
		t.Errorf("run scanned after %v, want every page of the table %v", scans, want)
	}
	if _, scans, _ := runScans(sweep, identities); len(scans) == 0 || !scans[0].IsZero() {
		t.Errorf("the run after a finished pass scanned after %v, want it to start over", scans)
	}
}

func TestForgetDeletedIdentities_RunStopsAtItsDeadlineAndResumes(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	owners := deletedOwnersInKeyOrder(t, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), identities, 2*ownerScanWindow+1)
	sweep := service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), silentSweepMetrics{}, silentCleaner{}, stallingRunBudget)
	identities.stallOnScan = 3

	forgotten, _, err := runScans(sweep, identities)
	if err != nil {
		t.Fatalf("Execute(first run) = %v, want a spent budget to end the run quietly", err)
	}
	if forgotten != 2*ownerScanWindow {
		t.Fatalf("first run forgot %d accounts before its deadline, want the %d on the two pages it finished", forgotten, 2*ownerScanWindow)
	}

	identities.stallOnScan = 0
	forgotten, scans, err := runScans(sweep, identities)
	if err != nil {
		t.Fatalf("Execute(second run): %v", err)
	}
	if !slices.Equal(scans, []ids.UserId{owners[2*ownerScanWindow-1]}) || forgotten != 1 {
		t.Fatalf("second run scanned after %v and forgot %d, want it to resume after the second page and erase the last owner", scans, forgotten)
	}
}

func newFailingOwnersRepo() *failingOwnersQueueRepo {
	return &failingOwnersQueueRepo{QueueStateRepo: memory.NewQueueStateRepo(), failing: map[ids.UserId]error{}, attempts: map[ids.UserId]int{}}
}

func TestForgetDeletedIdentities_PermanentlyFailingOwnerDoesNotBlockTheTable(t *testing.T) {
	repo := newFailingOwnersRepo()
	identities := newIdentityStore(repo.QueueStateRepo)
	owners := deletedOwnersInKeyOrder(t, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), identities, 2*ownerScanWindow+1)
	sweep := service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), silentSweepMetrics{}, silentCleaner{}, stallingRunBudget)
	dbDown := errors.New("connection refused")
	repo.failing[owners[0]] = dbDown
	identities.stallOnScan = 2

	forgotten, _, err := runScans(sweep, identities)
	if !errors.Is(err, dbDown) || forgotten != ownerScanWindow-1 || repo.attempts[owners[0]] != 1 {
		t.Fatalf("first run = (%d, %v) with %d attempts on the failing owner, want %d erased, the failure reported and 1 attempt",
			forgotten, err, repo.attempts[owners[0]], ownerScanWindow-1)
	}

	identities.stallOnScan = 0
	forgotten, scans, err := runScans(sweep, identities)
	if !errors.Is(err, dbDown) {
		t.Fatalf("second run = %v, want the failing owner reported again", err)
	}
	if repo.attempts[owners[0]] != 2 {
		t.Errorf("failing owner attempted %d times after two runs, want it retried every run", repo.attempts[owners[0]])
	}
	if len(scans) == 0 || scans[0] != owners[ownerScanWindow-1] {
		t.Fatalf("second run scanned after %v, want the cursor past the finished first page", scans)
	}
	if forgotten != ownerScanWindow+1 || repo.Len() != 1 {
		t.Fatalf("second run forgot %d and left %d, want the %d owners after the first page erased", forgotten, repo.Len(), ownerScanWindow+1)
	}

	delete(repo.failing, owners[0])
	if forgotten, _, err := runScans(sweep, identities); err != nil || forgotten != 1 || repo.Len() != 0 {
		t.Errorf("third run = (%d, %v) leaving %d, want only the failing owner retried and erased", forgotten, err, repo.Len())
	}
}

func TestForgetDeletedIdentities_RetriedOwnerLeavesTheRetrySetOnceErased(t *testing.T) {
	repo := newFailingOwnersRepo()
	identities := newIdentityStore(repo.QueueStateRepo)
	owners := deletedOwnersInKeyOrder(t, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), identities, 3)
	sweep := service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), silentSweepMetrics{}, silentCleaner{}, service.DefaultSweepRunBudget)
	dbDown := errors.New("connection refused")
	repo.failing[owners[1]] = dbDown

	if forgotten, _, err := runScans(sweep, identities); !errors.Is(err, dbDown) || forgotten != 2 {
		t.Fatalf("first run = (%d, %v), want the 2 healthy owners erased and the failure reported", forgotten, err)
	}

	delete(repo.failing, owners[1])
	forgotten, _, err := runScans(sweep, identities)
	if err != nil || forgotten != 1 || repo.Len() != 0 {
		t.Fatalf("second run = (%d, %v) leaving %d, want the failed owner erased on retry", forgotten, err, repo.Len())
	}
	if forgotten, _, err := runScans(sweep, identities); err != nil || forgotten != 0 || repo.attempts[owners[1]] != 2 {
		t.Errorf("third run = (%d, %v) with %d attempts on the erased owner, want it out of the retry set: 0 erased, 2 attempts",
			forgotten, err, repo.attempts[owners[1]])
	}
}

func TestForgetDeletedIdentities_FullRetrySetDropsAndCountsTheOverflow(t *testing.T) {
	repo := newFailingOwnersRepo()
	identities := newIdentityStore(repo.QueueStateRepo)
	owners := deletedOwnersInKeyOrder(t, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), identities, maxRetryOwners+1)
	spy := &sweepMetricsSpy{}
	sweep := service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), spy, silentCleaner{}, service.DefaultSweepRunBudget)
	for _, owner := range owners {
		repo.failing[owner] = errors.New("connection refused")
	}

	if _, _, err := runScans(sweep, identities); err == nil {
		t.Fatal("Execute must report the failed erases")
	}
	if spy.retryDropped != 1 {
		t.Errorf("%d drops counted, want the 1 owner past the cap of %d", spy.retryDropped, maxRetryOwners)
	}

	clear(repo.failing)
	identities.err = fmt.Errorf("query: %w", domain.ErrIdentityStoreUnavailable)
	if forgotten, _, _ := runScans(sweep, identities); forgotten != maxRetryOwners {
		t.Errorf("the retry pass alone erased %d owners, want the retry set capped at %d", forgotten, maxRetryOwners)
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
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("query: %w", domain.ErrIdentityStoreUnavailable)
	spy := &sweepMetricsSpy{}
	svc := service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), spy, silentCleaner{}, service.DefaultSweepRunBudget)

	if _, err := svc.Execute(context.Background()); !errors.Is(err, domain.ErrIdentityStoreUnavailable) {
		t.Fatalf("Execute = %v, want ErrIdentityStoreUnavailable", err)
	}

	if spy.idle != 1 || spy.erased != 0 {
		t.Errorf("idle=%d erased=%d, want 1 and 0", spy.idle, spy.erased)
	}
}

func TestForgetDeletedIdentities_ErasedAccountsAreCounted(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	user := saveQueueOf(t, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), identities, "search:x")
	identities.deleteAccount(user)
	spy := &sweepMetricsSpy{}
	svc := service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), spy, silentCleaner{}, service.DefaultSweepRunBudget)

	if _, err := svc.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if spy.erased != 1 || spy.idle != 0 {
		t.Errorf("idle=%d erased=%d, want 0 and 1", spy.idle, spy.erased)
	}
}

func TestForgetDeletedIdentities_UnreadableIdentityStoreFailsTheRun(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("query: %w", domain.ErrIdentityStoreUnavailable)
	spy := &sweepMetricsSpy{}
	svc := service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), spy, silentCleaner{}, service.DefaultSweepRunBudget)

	forgotten, err := svc.Execute(context.Background())

	if !errors.Is(err, domain.ErrIdentityStoreUnavailable) {
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
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	reaper := &reaperSpy{}
	svc := service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), silentSweepMetrics{}, reaper, service.DefaultSweepRunBudget)

	if _, err := svc.Execute(context.Background()); err != nil {
		t.Fatalf("Execute: %v", err)
	}

	if reaper.calls != 1 {
		t.Errorf("reaper ran %d times, want 1 even with no orphaned owners", reaper.calls)
	}
}

func TestForgetDeletedIdentities_ReapsWhenIdentityStoreIsUnavailable(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("query: %w", domain.ErrIdentityStoreUnavailable)
	reaper := &reaperSpy{}
	svc := service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), silentSweepMetrics{}, reaper, service.DefaultSweepRunBudget)

	if _, err := svc.Execute(context.Background()); !errors.Is(err, domain.ErrIdentityStoreUnavailable) {
		t.Fatalf("Execute = %v, want ErrIdentityStoreUnavailable", err)
	}

	if reaper.calls != 1 {
		t.Errorf("reaper ran %d times, want 1 while the identity store is down", reaper.calls)
	}
}

func TestForgetDeletedIdentities_ReapFailureDoesNotStopTheErasure(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	deleted := saveQueueOf(t, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), identities, "search:x")
	identities.deleteAccount(deleted)
	dbDown := errors.New("connection refused")
	svc := service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), silentSweepMetrics{}, &reaperSpy{err: dbDown}, service.DefaultSweepRunBudget)

	forgotten, err := svc.Execute(context.Background())

	if forgotten != 1 {
		t.Errorf("forgot %d accounts after a reap failure, want 1", forgotten)
	}
	if !errors.Is(err, dbDown) {
		t.Errorf("Execute = %v, want the reap failure reported", err)
	}
}

func TestForgetDeletedIdentities_OwnerScanDeadlineIsCounted(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	identities.err = fmt.Errorf("list owners without identity: %w", context.DeadlineExceeded)
	spy := &sweepMetricsSpy{}
	svc := service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), spy, silentCleaner{}, service.DefaultSweepRunBudget)

	_, err := svc.Execute(context.Background())

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Execute = %v, want the scan deadline reported", err)
	}
	if spy.scanTimeouts != 1 || spy.idle != 0 {
		t.Errorf("scanTimeouts=%d idle=%d, want 1 and 0", spy.scanTimeouts, spy.idle)
	}
}

func TestForgetDeletedIdentities_CancelledRunIsNotAScanTimeout(t *testing.T) {
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	identities.err = context.DeadlineExceeded
	spy := &sweepMetricsSpy{}
	svc := service.NewErasureSweeper(identities, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), spy, silentCleaner{}, service.DefaultSweepRunBudget)
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
	repo := memory.NewQueueStateRepo()
	identities := newIdentityStore(repo)
	const privateSourceId = "search:mac demarco"
	deleted := saveQueueOf(t, service.MustNewQueueService(repo, &fakeNowPlaying{}, time.Now), identities, privateSourceId)
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

func TestNewErasureSweeper_RefusesAMissingCollaboratorOrBudget(t *testing.T) {
	identities := &identityStore{queue: memory.NewQueueStateRepo(), live: map[uuid.UUID]bool{}}
	queues := service.MustNewQueueService(memory.NewQueueStateRepo(), &fakeNowPlaying{}, time.Now)
	metrics := silentSweepMetrics{}
	reaper := silentCleaner{}
	for name, build := range map[string]func(){
		"no identity lister": func() { service.NewErasureSweeper(nil, queues, metrics, reaper, time.Second) },
		"no queue service":   func() { service.NewErasureSweeper(identities, nil, metrics, reaper, time.Second) },
		"no metrics":         func() { service.NewErasureSweeper(identities, queues, nil, reaper, time.Second) },
		"no reaper":          func() { service.NewErasureSweeper(identities, queues, metrics, nil, time.Second) },
		"no run budget":      func() { service.NewErasureSweeper(identities, queues, metrics, reaper, 0) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("construction succeeded, want a panic at construction")
				}
			}()
			build()
		})
	}
}
