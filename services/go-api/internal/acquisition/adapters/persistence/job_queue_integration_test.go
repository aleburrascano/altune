package persistence

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	catalogpersistence "altune/go-api/internal/catalog/adapters/persistence"
	"altune/go-api/internal/catalog/domain"
	catalogports "altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func insertPendingTrackForUser(t *testing.T, pool *pgxpool.Pool, userID shared.UserId, availableAt time.Time) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userID, "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status, acquisition_available_at)
		 VALUES ($1, $2, $3, $4, $5, $6, 'pending', $7)`,
		track.ID.UUID(), track.UserId.UUID(), track.Title, track.Artist, track.Album, uuid.NewString(), availableAt)
	if err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
	})
	return track
}

func insertPendingTrack(t *testing.T, pool *pgxpool.Pool, availableAt time.Time) *domain.Track {
	t.Helper()
	return insertPendingTrackForUser(t, pool, shared.NewUserId(uuid.New()), availableAt)
}

func expireLease(t *testing.T, pool *pgxpool.Pool, trackID domain.TrackId) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE tracks SET acquisition_lease_until = now() - interval '1 second' WHERE id = $1`, trackID.UUID()); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
}

func leaseUntil(t *testing.T, pool *pgxpool.Pool, trackID domain.TrackId) time.Time {
	t.Helper()
	var until time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT acquisition_lease_until FROM tracks WHERE id = $1`, trackID.UUID()).Scan(&until); err != nil {
		t.Fatalf("select lease: %v", err)
	}
	return until
}

func TestPgxJobQueue_ClaimSkipsUnavailableAndLeasedRows(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	future := insertPendingTrack(t, pool, time.Now().Add(time.Hour))
	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))

	job, err := queue.Claim(ctx, 2*time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want a job", err)
	}
	if job.TrackID != track.ID {
		t.Errorf("Claim returned track %s, want the available one %s (not the future-scheduled %s)", job.TrackID, track.ID, future.ID)
	}
	if job.Attempts != 1 {
		t.Errorf("Attempts = %d, want 1", job.Attempts)
	}

	if _, err := queue.Claim(ctx, 2*time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Errorf("second Claim = %v, want ErrNoJobAvailable (track is leased, future one is not yet due)", err)
	}
}

func TestPgxJobQueue_ClaimReportsHowLongTheJobWaited(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)

	insertPendingTrack(t, pool, time.Now().Add(-90*time.Second))

	job, err := queue.Claim(context.Background(), 2*time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want a job", err)
	}
	if job.Waited < 89*time.Second || job.Waited > 2*time.Minute {
		t.Errorf("Waited = %v, want about 90s", job.Waited)
	}
}

func TestPgxJobQueue_ConcurrentClaimersNeverDoubleClaim(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	ctx := context.Background()

	const jobs = 12
	tracks := make(map[domain.TrackId]bool, jobs)
	for range jobs {
		track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
		tracks[track.ID] = false
	}

	var mu sync.Mutex
	var claimed atomic.Int32
	var wg sync.WaitGroup
	for range jobs * 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			queue := NewPgxJobQueue(pool)
			job, err := queue.Claim(ctx, 2*time.Minute)
			if errors.Is(err, acqdomain.ErrNoJobAvailable) {
				return
			}
			if err != nil {
				t.Errorf("Claim: %v", err)
				return
			}
			mu.Lock()
			seen, known := tracks[job.TrackID]
			if !known {
				t.Errorf("Claim returned unexpected track %s", job.TrackID)
			} else if seen {
				t.Errorf("track %s claimed twice", job.TrackID)
			}
			tracks[job.TrackID] = true
			mu.Unlock()
			claimed.Add(1)
		}()
	}
	wg.Wait()

	if got := claimed.Load(); got != jobs {
		t.Errorf("claimed = %d, want %d (every job claimed exactly once)", got, jobs)
	}
}

func TestPgxJobQueue_LapsedLeaseIsReclaimed(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))

	first, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("first Claim = %v, want a job", err)
	}
	if first.TrackID != track.ID {
		t.Fatalf("first Claim returned %s, want %s", first.TrackID, track.ID)
	}

	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Errorf("Claim while leased = %v, want ErrNoJobAvailable", err)
	}

	expireLease(t, pool, track.ID)

	second, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim after lease lapsed = %v, want a job", err)
	}
	if second.TrackID != track.ID {
		t.Fatalf("second Claim returned %s, want reclaimed %s", second.TrackID, track.ID)
	}
	if second.Attempts != 2 {
		t.Errorf("Attempts after reclaim = %d, want 2", second.Attempts)
	}
}

func TestPgxJobQueue_HeartbeatExtendsLeaseAndFailsOnceLapsed(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := queue.Heartbeat(ctx, track.ID, job.Fence, time.Hour); err != nil {
		t.Fatalf("Heartbeat = %v, want nil", err)
	}
	if until := leaseUntil(t, pool, track.ID); time.Until(until) < 30*time.Minute {
		t.Errorf("lease after an hour-long heartbeat ends in %s, want about an hour", time.Until(until))
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Errorf("Claim after heartbeat extended the lease = %v, want ErrNoJobAvailable", err)
	}

	if err := queue.Settle(ctx, track.ID, job.Fence); err != nil {
		t.Fatalf("Settle = %v, want nil", err)
	}
	if err := queue.Heartbeat(ctx, track.ID, job.Fence, time.Minute); err == nil {
		t.Error("Heartbeat after Settle cleared the lease = nil error, want an error")
	}
}

func TestPgxJobQueue_ReleaseSchedulesRetryAtAvailableAt(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := queue.Release(ctx, track.ID, job.Fence, time.Hour); err != nil {
		t.Fatalf("Release = %v, want nil", err)
	}

	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Errorf("Claim before the backoff elapses = %v, want ErrNoJobAvailable", err)
	}

	if err := queue.Release(ctx, track.ID, job.Fence, 0); err != nil {
		t.Fatalf("Release (immediate retry) = %v, want nil", err)
	}
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Errorf("Claim after immediate retry = %v, want a job", err)
	}
}

func TestPgxJobQueue_EnqueueMarksPendingAndClaimable(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertFailedTrack(t, pool)

	if err := queue.Enqueue(ctx, track.ID, ports.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}

	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim after Enqueue = %v, want a job", err)
	}
	if job.TrackID != track.ID {
		t.Fatalf("Claim returned %s, want the enqueued track %s", job.TrackID, track.ID)
	}
	if job.Kind != ports.JobKindReplace {
		t.Errorf("Kind = %q, want %q", job.Kind, ports.JobKindReplace)
	}
}

func TestPgxJobQueue_FairAcrossUsersFavoursTheUserWithNoInFlightJob(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	busyUser := shared.NewUserId(uuid.New())
	idleUser := shared.NewUserId(uuid.New())

	busyEarlier := insertPendingTrackForUser(t, pool, busyUser, time.Now().Add(-2*time.Second))
	busyLater := insertPendingTrackForUser(t, pool, busyUser, time.Now().Add(-time.Second))
	idleLater := insertPendingTrackForUser(t, pool, idleUser, time.Now())

	first, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("first Claim = %v", err)
	}
	if first.TrackID != busyEarlier.ID {
		t.Fatalf("first Claim returned %s, want the earliest-available %s", first.TrackID, busyEarlier.ID)
	}

	second, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("second Claim = %v", err)
	}
	if second.TrackID != idleLater.ID {
		t.Errorf("second Claim returned %s (queued at %s), want the idle user's job %s ahead of the busy user's second job %s",
			second.TrackID, busyLater.ID, idleLater.ID, busyLater.ID)
	}
}

func TestPgxJobQueue_ZombieHeartbeatFailsAfterReclaimAndLeavesNewOwnerAlone(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))

	zombie, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("first Claim (A) = %v, want a job", err)
	}
	expireLease(t, pool, track.ID)

	newOwner, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("second Claim (B, after A's lease lapsed) = %v, want a job", err)
	}
	if newOwner.TrackID != track.ID {
		t.Fatalf("second Claim returned %s, want reclaimed %s", newOwner.TrackID, track.ID)
	}
	if newOwner.Attempts == zombie.Attempts {
		t.Fatalf("reclaim did not advance the fence: zombie.Attempts=%d newOwner.Attempts=%d", zombie.Attempts, newOwner.Attempts)
	}

	if err := queue.Heartbeat(ctx, track.ID, zombie.Fence, time.Minute); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Errorf("zombie Heartbeat = %v, want ErrLeaseLost", err)
	}
	if err := queue.Release(ctx, track.ID, zombie.Fence, 0); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Errorf("zombie Release = %v, want ErrLeaseLost", err)
	}
	if err := queue.Settle(ctx, track.ID, zombie.Fence); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Errorf("zombie Settle = %v, want ErrLeaseLost", err)
	}

	if err := queue.Heartbeat(ctx, track.ID, newOwner.Fence, time.Minute); err != nil {
		t.Errorf("legitimate owner's Heartbeat after zombie calls = %v, want nil (row must be untouched)", err)
	}
	if err := queue.Settle(ctx, track.ID, newOwner.Fence); err != nil {
		t.Errorf("legitimate owner's Settle after zombie calls = %v, want nil (row must be untouched)", err)
	}
}

func TestPgxJobQueue_SettleAfterAReEnqueueLeavesThePendingJobClaimable(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	owner, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want a job", err)
	}
	if err := queue.Enqueue(ctx, track.ID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue while leased = %v, want nil", err)
	}
	if err := queue.Settle(ctx, track.ID, owner.Fence); err != nil {
		t.Fatalf("owner Settle = %v, want nil", err)
	}

	next, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim after settling a still-pending job = %v, want the job back", err)
	}
	if next.TrackID != track.ID {
		t.Errorf("Claim returned %s, want the re-enqueued %s", next.TrackID, track.ID)
	}
}

func TestPgxJobQueue_SettleOfAFinishedJobClearsItsSchedule(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	owner, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want a job", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tracks SET acquisition_status = 'ready' WHERE id = $1`, track.ID.UUID()); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	if err := queue.Settle(ctx, track.ID, owner.Fence); err != nil {
		t.Fatalf("Settle = %v, want nil", err)
	}

	var availableAt, leaseUntil *time.Time
	if err := pool.QueryRow(ctx, `SELECT acquisition_available_at, acquisition_lease_until FROM tracks WHERE id = $1`, track.ID.UUID()).
		Scan(&availableAt, &leaseUntil); err != nil {
		t.Fatalf("select track: %v", err)
	}
	if availableAt != nil || leaseUntil != nil {
		t.Errorf("settled ready row keeps available_at=%v lease_until=%v, want both nil", availableAt, leaseUntil)
	}
}

func TestPgxJobQueue_EnqueueKeepsALiveLeaseSoNoSecondWorkerClaims(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	owner, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want a job", err)
	}

	if err := queue.Enqueue(ctx, track.ID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue while leased = %v, want nil", err)
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Fatalf("Claim while the owner's lease is live = %v, want ErrNoJobAvailable", err)
	}
	if err := queue.Heartbeat(ctx, track.ID, owner.Fence, time.Minute); err != nil {
		t.Errorf("owner Heartbeat after a re-enqueue = %v, want nil", err)
	}
}

func TestPgxJobQueue_EnqueueOnLiveLeaseOfDifferentKindConflicts(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := queue.Enqueue(ctx, track.ID, ports.JobKindReplace, 0); !errors.Is(err, acqdomain.ErrJobKindConflict) {
		t.Errorf("Enqueue replace over a running acquire = %v, want ErrJobKindConflict", err)
	}
}

func insertReadyTrack(t *testing.T, pool *pgxpool.Pool) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(shared.NewUserId(uuid.New()), "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	_, err = pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status, audio_ref) VALUES ($1, $2, $3, $4, $5, $6, 'ready', 'audio/old.mp3')`,
		track.ID.UUID(), track.UserId.UUID(), track.Title, track.Artist, track.Album, uuid.NewString())
	if err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
	})
	return track
}

func markRowFailed(t *testing.T, pool *pgxpool.Pool, trackID domain.TrackId) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE tracks SET acquisition_status = 'failed', failure_reason = 'audio missing', version = version + 1 WHERE id = $1`,
		trackID.UUID()); err != nil {
		t.Fatalf("mark row failed: %v", err)
	}
}

func acquisitionStatus(t *testing.T, pool *pgxpool.Pool, trackID domain.TrackId) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT acquisition_status FROM tracks WHERE id = $1`, trackID.UUID()).Scan(&status); err != nil {
		t.Fatalf("select status: %v", err)
	}
	return status
}

func TestPgxJobQueue_ReplaceOfAReadyTrackKeepsItReadyThroughClaimAndSettle(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertReadyTrack(t, pool)

	if err := queue.Enqueue(ctx, track.ID, ports.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "ready" {
		t.Fatalf("status after Enqueue = %q, want ready so the old audio keeps streaming", got)
	}

	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want the replace job", err)
	}
	if job.TrackID != track.ID || job.Kind != ports.JobKindReplace {
		t.Fatalf("Claim = %+v, want a replace job for %s", job, track.ID)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "ready" {
		t.Errorf("status when claimed = %q, want ready", got)
	}
	if err := queue.Heartbeat(ctx, track.ID, job.Fence, time.Minute); err != nil {
		t.Errorf("Heartbeat on a ready track's replace job = %v, want nil", err)
	}

	if err := queue.Settle(ctx, track.ID, job.Fence); err != nil {
		t.Fatalf("Settle = %v", err)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "ready" {
		t.Errorf("status after a failed replace settles = %q, want ready", got)
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Errorf("Claim after Settle = %v, want ErrNoJobAvailable", err)
	}
}

func TestPgxJobQueue_EnqueueOfAcquireOnAReadyTrackLeavesItReady(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)

	track := insertReadyTrack(t, pool)

	if err := queue.Enqueue(context.Background(), track.ID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "ready" {
		t.Errorf("status = %q, want ready", got)
	}
	if _, err := queue.Claim(context.Background(), time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Errorf("Claim = %v, want ErrNoJobAvailable", err)
	}
}

func TestPgxJobQueue_SecondReplaceEnqueueWhileLeasedIsANoop(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertReadyTrack(t, pool)
	markRowFailed(t, pool, track.ID)
	if err := queue.Enqueue(ctx, track.ID, ports.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := queue.Enqueue(ctx, track.ID, ports.JobKindAcquire, 0); !errors.Is(err, acqdomain.ErrJobKindConflict) {
		t.Errorf("Enqueue of another kind while a replace is leased = %v, want ErrJobKindConflict", err)
	}
}

type panickingAcquirer struct{ runs atomic.Int32 }

func (a *panickingAcquirer) Execute(context.Context, shared.UserId, domain.TrackId) error {
	a.runs.Add(1)
	panic("boom")
}

func (a *panickingAcquirer) ExecuteReplace(ctx context.Context, u shared.UserId, t domain.TrackId) error {
	return a.Execute(ctx, u, t)
}

func (a *panickingAcquirer) RefuseQueued(context.Context, shared.UserId, domain.TrackId) {}

func TestPgxJobQueue_PanickedJobRowIsNotClaimableAgainImmediately(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	acq := &panickingAcquirer{}
	var wg sync.WaitGroup
	scheduler := service.NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1),
		service.WithJobQueue(queue), service.WithPollInterval(5*time.Millisecond))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	deadline := time.Now().Add(5 * time.Second)
	for acq.runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)

	if got := acq.runs.Load(); got != 1 {
		t.Errorf("runs = %d, want 1 (a panicked job must back off, not be re-claimed)", got)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "pending" {
		t.Errorf("status = %q, want pending until the attempt cap", got)
	}
}

func TestPgxJobQueue_ClaimPlanUsesScheduledAvailableAtIndex(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	ctx := context.Background()
	insertPendingTrack(t, pool, time.Now().Add(-time.Minute))

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}
	rows, err := tx.Query(ctx, "EXPLAIN "+claimJobSQL, 30.0)
	if err != nil {
		t.Fatalf("explain claim: %v", err)
	}
	defer rows.Close()
	var plan strings.Builder
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			t.Fatalf("scan plan: %v", err)
		}
		plan.WriteString(line + "\n")
	}
	if !strings.Contains(plan.String(), "idx_tracks_acquisition_available_at_scheduled") {
		t.Errorf("claim plan does not use the scheduled available_at index:\n%s", plan.String())
	}
}

func TestPgxJobQueue_PendingDepthCountsUnleasedJobsAndReportsOldestAge(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	baselinePending, _, err := queue.PendingDepth(ctx)
	if err != nil {
		t.Fatalf("baseline PendingDepth = %v", err)
	}
	insertPendingTrack(t, pool, time.Now().Add(-10*time.Minute))
	insertPendingTrack(t, pool, time.Now().Add(-time.Minute))
	insertPendingTrack(t, pool, time.Now().Add(time.Hour))
	leased := insertPendingTrack(t, pool, time.Now().Add(-time.Minute))
	if _, err := pool.Exec(ctx,
		`UPDATE tracks SET acquisition_lease_until = now() + interval '2 minutes' WHERE id = $1`, leased.ID.UUID()); err != nil {
		t.Fatalf("lease track: %v", err)
	}

	pending, oldest, err := queue.PendingDepth(ctx)
	if err != nil {
		t.Fatalf("PendingDepth = %v", err)
	}
	if pending-baselinePending != 3 {
		t.Errorf("pending grew by %d, want 3 (two due plus one scheduled, leased excluded)", pending-baselinePending)
	}
	if oldest < 10*time.Minute {
		t.Errorf("oldest age = %v, want at least 10m", oldest)
	}
}

func TestPgxJobQueue_ReEnqueueAfterPriorClaimsStartsANewRunButKeepsTheFence(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	var stale ports.Job
	for i := 0; i < 3; i++ {
		job, err := queue.Claim(ctx, time.Minute)
		if err != nil {
			t.Fatalf("Claim #%d = %v", i+1, err)
		}
		stale = job
		expireLease(t, pool, track.ID)
	}

	if err := queue.Enqueue(ctx, track.ID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("re-Enqueue = %v", err)
	}
	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim after re-Enqueue = %v", err)
	}

	if job.Run != 1 || job.Attempts != 4 {
		t.Errorf("Run/Attempts = %d/%d, want 1/4", job.Run, job.Attempts)
	}
	if err := queue.Settle(ctx, track.ID, stale.Fence); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Errorf("stale Settle = %v, want ErrLeaseLost", err)
	}
}

func databaseClock(t *testing.T, pool *pgxpool.Pool) time.Time {
	t.Helper()
	var now time.Time
	if err := pool.QueryRow(context.Background(), `SELECT clock_timestamp()`).Scan(&now); err != nil {
		t.Fatalf("select database clock: %v", err)
	}
	return now
}

func availableAt(t *testing.T, pool *pgxpool.Pool, trackID domain.TrackId) time.Time {
	t.Helper()
	var at time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT acquisition_available_at FROM tracks WHERE id = $1`, trackID.UUID()).Scan(&at); err != nil {
		t.Fatalf("select available_at: %v", err)
	}
	return at
}

func TestPgxJobQueue_ReleaseDelayIsMeasuredOnTheDatabaseClock(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now())
	if _, err := pool.Exec(ctx,
		`UPDATE tracks SET acquisition_available_at = now() - interval '1 minute' WHERE id = $1`, track.ID.UUID()); err != nil {
		t.Fatalf("backdate on the database clock: %v", err)
	}
	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	before := databaseClock(t, pool)
	if err := queue.Release(ctx, track.ID, job.Fence, time.Hour); err != nil {
		t.Fatalf("Release = %v, want nil", err)
	}
	after := databaseClock(t, pool)

	got := availableAt(t, pool, track.ID)
	if got.Before(before.Add(time.Hour)) || got.After(after.Add(time.Hour)) {
		t.Errorf("available_at = %v, want within [%v, %v] (database now() plus one hour)", got, before.Add(time.Hour), after.Add(time.Hour))
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Errorf("Claim inside the delay = %v, want ErrNoJobAvailable", err)
	}

	before = databaseClock(t, pool)
	if err := queue.Release(ctx, track.ID, job.Fence, 0); err != nil {
		t.Fatalf("Release (no delay) = %v, want nil", err)
	}
	after = databaseClock(t, pool)
	if got := availableAt(t, pool, track.ID); got.Before(before) || got.After(after) {
		t.Errorf("available_at after a zero delay = %v, want within [%v, %v]", got, before, after)
	}
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Errorf("Claim after a zero delay = %v, want a job", err)
	}
}

func TestPgxJobQueue_FencedUpdatesWrapErrorsWithTheirOperation(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)

	if dbCallTimeout != 5*time.Second {
		t.Errorf("dbCallTimeout = %s, want 5s", dbCallTimeout)
	}

	missing := domain.TrackIdFromUUID(uuid.New())
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()

	cases := []struct {
		op  string
		run func(ctx context.Context) error
	}{
		{"heartbeat", func(ctx context.Context) error { return queue.Heartbeat(ctx, missing, 1, time.Minute) }},
		{"release", func(ctx context.Context) error { return queue.Release(ctx, missing, 1, time.Minute) }},
		{"settle", func(ctx context.Context) error { return queue.Settle(ctx, missing, 1) }},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			err := tc.run(context.Background())
			if !errors.Is(err, acqdomain.ErrLeaseLost) {
				t.Fatalf("zero rows = %v, want ErrLeaseLost", err)
			}
			want := tc.op + " acquisition job for track " + missing.String() + ": " + acqdomain.ErrLeaseLost.Error()
			if err.Error() != want {
				t.Errorf("zero-rows error = %q, want %q", err.Error(), want)
			}

			err = tc.run(cancelled)
			if err == nil || errors.Is(err, acqdomain.ErrLeaseLost) {
				t.Fatalf("cancelled context = %v, want a database error", err)
			}
			if prefix := tc.op + " acquisition job: "; !strings.HasPrefix(err.Error(), prefix) {
				t.Errorf("database error = %q, want prefix %q", err.Error(), prefix)
			}
			if !errors.Is(err, context.Canceled) {
				t.Errorf("database error = %v, want it to wrap context.Canceled", err)
			}
		})
	}
}

func TestPgxJobQueue_AnUpgradeKeepsTheTrackReadyWhileItIsQueued(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("keep")})

	if err := queue.Enqueue(context.Background(), id, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue upgrade = %v, want nil", err)
	}

	var status string
	if err := pool.QueryRow(context.Background(), `SELECT acquisition_status FROM tracks WHERE id = $1`, id.UUID()).Scan(&status); err != nil {
		t.Fatalf("read status: %v", err)
	}
	if status != "ready" {
		t.Errorf("status while an upgrade is queued = %q, want ready (the old file keeps playing)", status)
	}
}

func TestPgxJobQueue_AnUpgradeIsNeverQueuedForATrackWithoutAPlayableFile(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	track := insertPendingTrack(t, pool, time.Now().Add(time.Hour))

	err := queue.Enqueue(context.Background(), track.ID, ports.JobKindUpgrade, 0)

	if err == nil {
		t.Fatal("Enqueue upgrade on a pending track = nil, want it refused")
	}
	var kind string
	if err := pool.QueryRow(context.Background(), `SELECT acquisition_job_kind FROM tracks WHERE id = $1`, track.ID.UUID()).Scan(&kind); err != nil {
		t.Fatalf("read kind: %v", err)
	}
	if kind == string(ports.JobKindUpgrade) {
		t.Error("the refused enqueue still marked the track as an upgrade job")
	}
}

func TestPgxJobQueue_UserAddsKeepPriorityOverUpgrades(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()
	upgrading := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("up")})
	if err := queue.Enqueue(ctx, upgrading, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue upgrade = %v", err)
	}
	added := insertPendingTrack(t, pool, time.Now().Add(-time.Second))

	if _, err := queue.ClaimUpgrade(ctx, time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Fatalf("ClaimUpgrade while a user add waits = %v, want ErrNoJobAvailable", err)
	}
	userJob, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want the user's add (never the upgrade)", err)
	}
	if userJob.TrackID != added.ID || userJob.Kind != ports.JobKindAcquire {
		t.Errorf("Claim returned %+v, want the user add %s", userJob, added.ID)
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Fatalf("second Claim = %v, want ErrNoJobAvailable: the upgrade is not a user job", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tracks SET acquisition_status = 'ready' WHERE id = $1`, added.ID.UUID()); err != nil {
		t.Fatalf("mark the add ready: %v", err)
	}
	if err := queue.Settle(ctx, userJob.TrackID, userJob.Fence); err != nil {
		t.Fatalf("Settle = %v", err)
	}

	upgrade, err := queue.ClaimUpgrade(ctx, time.Minute)
	if err != nil {
		t.Fatalf("ClaimUpgrade once no user add waits = %v, want the upgrade", err)
	}
	if upgrade.TrackID != upgrading || upgrade.Kind != ports.JobKindUpgrade {
		t.Errorf("ClaimUpgrade returned %+v, want the upgrade of %s", upgrade, upgrading)
	}
}

func TestPgxJobQueue_AnUpgradeNeverOverwritesAQueuedUserReplace(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("replaced")})
	if err := queue.Enqueue(ctx, id, ports.JobKindReplace, time.Hour); err != nil {
		t.Fatalf("Enqueue replace = %v", err)
	}

	err := queue.Enqueue(ctx, id, ports.JobKindUpgrade, 0)

	if !errors.Is(err, acqdomain.ErrJobKindConflict) {
		t.Fatalf("Enqueue upgrade over a queued replace = %v, want ErrJobKindConflict", err)
	}
	var kind string
	var queued bool
	if err := pool.QueryRow(ctx, `SELECT acquisition_job_kind, acquisition_available_at > now() + interval '30 minutes' FROM tracks WHERE id = $1`, id.UUID()).Scan(&kind, &queued); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if kind != string(ports.JobKindReplace) || !queued {
		t.Errorf("kind = %q, queued for later = %v, want the user's replace untouched", kind, queued)
	}
}

func TestPgxJobQueue_AUserReplaceTakesOverARunningUpgrade(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("running")})
	if err := queue.Enqueue(ctx, id, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue upgrade = %v", err)
	}
	upgrade, err := queue.ClaimUpgrade(ctx, time.Minute)
	if err != nil || upgrade.TrackID != id {
		t.Fatalf("ClaimUpgrade = %+v, %v, want the upgrade of the track", upgrade, err)
	}

	if err := queue.Enqueue(ctx, id, ports.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue replace over a running upgrade = %v, want the user's job to win", err)
	}

	if err := queue.Heartbeat(ctx, id, upgrade.Fence, time.Minute); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Errorf("upgrade Heartbeat = %v, want ErrLeaseLost so the upgrade stops", err)
	}
	replace, err := queue.Claim(ctx, time.Minute)
	if err != nil || replace.TrackID != id || replace.Kind != ports.JobKindReplace {
		t.Errorf("Claim = %+v, %v, want the user's replace right after", replace, err)
	}
}

func TestPgxJobQueue_AnUpgradeLosesItsLeaseTheMomentAReplaceTakesOver(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("running")})
	if err := queue.Enqueue(ctx, id, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue upgrade = %v", err)
	}
	upgrade, err := queue.ClaimUpgrade(ctx, time.Minute)
	if err != nil {
		t.Fatalf("ClaimUpgrade = %v", err)
	}
	if err := queue.VerifyLease(ctx, id, upgrade.Fence); err != nil {
		t.Fatalf("VerifyLease while the upgrade holds the lease = %v, want nil", err)
	}

	if err := queue.Enqueue(ctx, id, ports.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue replace = %v", err)
	}

	if err := queue.VerifyLease(ctx, id, upgrade.Fence); !errors.Is(err, acqdomain.ErrLeaseLost) {
		t.Errorf("VerifyLease after the takeover = %v, want ErrLeaseLost", err)
	}
}

func TestPgxJobQueue_EnqueueMakesAStaleCatalogWriteConflict(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	repo := catalogpersistence.NewPgxTrackRepository(pool)
	ctx := context.Background()

	snap := insertFailedTrack(t, pool)
	loaded, err := repo.GetByID(ctx, snap.ID, snap.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}

	if err := queue.Enqueue(ctx, snap.ID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}

	err = repo.Update(ctx, loaded, loaded.Version)
	if !errors.Is(err, catalogports.ErrTrackVersionConflict) {
		t.Fatalf("stale Update = %v, want ErrTrackVersionConflict", err)
	}
	if got := acquisitionStatus(t, pool, snap.ID); got != "pending" {
		t.Errorf("status = %q, want pending (the stale write must not restore failed)", got)
	}
}

func TestPgxJobQueue_ClaimMakesAStaleCatalogWriteConflict(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	repo := catalogpersistence.NewPgxTrackRepository(pool)
	ctx := context.Background()

	pending := insertPendingTrack(t, pool, time.Now().Add(-time.Minute))
	loaded, err := repo.GetByID(ctx, pending.ID, pending.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}

	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim = %v, want a job", err)
	}

	err = repo.Update(ctx, loaded, loaded.Version)
	if !errors.Is(err, catalogports.ErrTrackVersionConflict) {
		t.Fatalf("stale Update = %v, want ErrTrackVersionConflict", err)
	}
}

func TestPgxJobQueue_RetryBetweenAudioMissingLoadAndWriteLeavesTheTrackPendingAndClaimable(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	repo := catalogpersistence.NewPgxTrackRepository(pool)
	ctx := context.Background()

	track := insertReadyTrack(t, pool)
	loaded, err := repo.GetByID(ctx, track.ID, track.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}
	if err := loaded.MarkFailed("audio missing"); err != nil {
		t.Fatalf("MarkFailed = %v", err)
	}

	markRowFailed(t, pool, track.ID)
	if err := queue.Enqueue(ctx, track.ID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}

	if err := repo.Update(ctx, loaded, loaded.Version); !errors.Is(err, catalogports.ErrTrackVersionConflict) {
		t.Fatalf("stale audio-missing Update = %v, want ErrTrackVersionConflict", err)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "pending" {
		t.Errorf("status = %q, want pending (the retry must not be written back to failed)", got)
	}
	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want the retried job", err)
	}
	if job.TrackID != track.ID {
		t.Errorf("Claim = %s, want %s", job.TrackID, track.ID)
	}
}

func TestPgxJobQueue_ReplaceEnqueueMakesAStaleCatalogWriteConflict(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	repo := catalogpersistence.NewPgxTrackRepository(pool)
	ctx := context.Background()

	track := insertReadyTrack(t, pool)
	loaded, err := repo.GetByID(ctx, track.ID, track.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}
	if err := loaded.MarkFailed("audio missing"); err != nil {
		t.Fatalf("MarkFailed = %v", err)
	}

	if err := queue.Enqueue(ctx, track.ID, ports.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue replace = %v, want nil", err)
	}

	if err := repo.Update(ctx, loaded, loaded.Version); !errors.Is(err, catalogports.ErrTrackVersionConflict) {
		t.Fatalf("stale Update after a replace enqueue = %v, want ErrTrackVersionConflict", err)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "ready" {
		t.Errorf("status = %q, want ready", got)
	}
}

func TestPgxJobQueue_SecondEnqueueMakesAWriterLoadedBetweenTheTwoConflict(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	repo := catalogpersistence.NewPgxTrackRepository(pool)
	ctx := context.Background()

	snap := insertFailedTrack(t, pool)
	if err := queue.Enqueue(ctx, snap.ID, ports.JobKindAcquire, time.Hour); err != nil {
		t.Fatalf("first Enqueue = %v", err)
	}
	loaded, err := repo.GetByID(ctx, snap.ID, snap.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}
	if err := queue.Enqueue(ctx, snap.ID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("second Enqueue = %v", err)
	}

	if err := repo.Update(ctx, loaded, loaded.Version); !errors.Is(err, catalogports.ErrTrackVersionConflict) {
		t.Fatalf("Update loaded between two enqueues = %v, want ErrTrackVersionConflict", err)
	}
}

func TestPgxJobQueue_ReclaimOfALapsedLeaseMakesAStaleCatalogWriteConflict(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	repo := catalogpersistence.NewPgxTrackRepository(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Minute))
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("first Claim = %v", err)
	}
	expireLease(t, pool, track.ID)
	loaded, err := repo.GetByID(ctx, track.ID, track.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}

	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("reclaim = %v, want the lapsed job", err)
	}

	if err := repo.Update(ctx, loaded, loaded.Version); !errors.Is(err, catalogports.ErrTrackVersionConflict) {
		t.Fatalf("Update loaded before the reclaim = %v, want ErrTrackVersionConflict", err)
	}
}

func TestPgxJobQueue_ClaimOfAReplaceJobMakesAStaleCatalogWriteConflict(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	repo := catalogpersistence.NewPgxTrackRepository(pool)
	ctx := context.Background()

	track := insertReadyTrack(t, pool)
	if err := queue.Enqueue(ctx, track.ID, ports.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue replace = %v", err)
	}
	loaded, err := repo.GetByID(ctx, track.ID, track.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}

	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim = %v, want the replace job", err)
	}

	if err := repo.Update(ctx, loaded, loaded.Version); !errors.Is(err, catalogports.ErrTrackVersionConflict) {
		t.Fatalf("Update loaded before the replace claim = %v, want ErrTrackVersionConflict", err)
	}
}

func TestPgxJobQueue_CatalogWriteThatRereadsAfterEnqueueAndClaimLands(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	repo := catalogpersistence.NewPgxTrackRepository(pool)
	ctx := context.Background()

	snap := insertFailedTrack(t, pool)
	stale, err := repo.GetByID(ctx, snap.ID, snap.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}
	if err := queue.Enqueue(ctx, snap.ID, ports.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim = %v", err)
	}
	if err := repo.Update(ctx, stale, stale.Version); !errors.Is(err, catalogports.ErrTrackVersionConflict) {
		t.Fatalf("stale Update = %v, want ErrTrackVersionConflict", err)
	}

	fresh, err := repo.GetByID(ctx, snap.ID, snap.UserId)
	if err != nil {
		t.Fatalf("re-read GetByID = %v", err)
	}
	if fresh.AcquisitionStartedAt == nil {
		t.Errorf("re-read AcquisitionStartedAt = nil, want the claim's start time")
	}
	if err := repo.Update(ctx, fresh, fresh.Version); err != nil {
		t.Fatalf("Update after a re-read = %v, want nil", err)
	}
	if got := acquisitionStatus(t, pool, snap.ID); got != "pending" {
		t.Errorf("status = %q, want pending", got)
	}
}

func TestPgxJobQueue_EnqueueRacingAStaleCatalogWriteAlwaysEndsPending(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	repo := catalogpersistence.NewPgxTrackRepository(pool)
	ctx := context.Background()

	for i := 0; i < 40; i++ {
		track := insertReadyTrack(t, pool)
		loaded, err := repo.GetByID(ctx, track.ID, track.UserId)
		if err != nil {
			t.Fatalf("GetByID = %v", err)
		}
		if err := loaded.MarkFailed("audio missing"); err != nil {
			t.Fatalf("MarkFailed = %v", err)
		}
		markRowFailed(t, pool, track.ID)

		var wg sync.WaitGroup
		var enqueueErr, updateErr error
		start := make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-start; enqueueErr = queue.Enqueue(ctx, track.ID, ports.JobKindAcquire, 0) }()
		go func() { defer wg.Done(); <-start; updateErr = repo.Update(ctx, loaded, loaded.Version) }()
		close(start)
		wg.Wait()

		if enqueueErr != nil {
			t.Fatalf("round %d: Enqueue = %v, want nil", i, enqueueErr)
		}
		if updateErr != nil && !errors.Is(updateErr, catalogports.ErrTrackVersionConflict) {
			t.Fatalf("round %d: Update = %v, want nil or ErrTrackVersionConflict", i, updateErr)
		}
		if got := acquisitionStatus(t, pool, track.ID); got != "pending" {
			t.Fatalf("round %d: status = %q (update err %v), want pending whichever write ran first", i, got, updateErr)
		}
	}
}

func TestPgxJobQueue_ClaimRacingAStaleCatalogWriteKeepsTheStartTime(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	repo := catalogpersistence.NewPgxTrackRepository(pool)
	ctx := context.Background()

	for i := 0; i < 40; i++ {
		track := insertPendingTrack(t, pool, time.Now().Add(-time.Minute))
		loaded, err := repo.GetByID(ctx, track.ID, track.UserId)
		if err != nil {
			t.Fatalf("GetByID = %v", err)
		}

		var wg sync.WaitGroup
		var claimErr, updateErr error
		start := make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, claimErr = queue.Claim(ctx, time.Minute) }()
		go func() { defer wg.Done(); <-start; updateErr = repo.Update(ctx, loaded, loaded.Version) }()
		close(start)
		wg.Wait()

		if errors.Is(claimErr, acqdomain.ErrNoJobAvailable) {
			_, _ = pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
			continue
		}
		if claimErr != nil {
			t.Fatalf("round %d: Claim = %v, want a job or ErrNoJobAvailable", i, claimErr)
		}
		if updateErr != nil && !errors.Is(updateErr, catalogports.ErrTrackVersionConflict) {
			t.Fatalf("round %d: Update = %v, want nil or ErrTrackVersionConflict", i, updateErr)
		}
		reread, err := repo.GetByID(ctx, track.ID, track.UserId)
		if err != nil {
			t.Fatalf("round %d: re-read = %v", i, err)
		}
		if reread.AcquisitionStartedAt == nil {
			t.Fatalf("round %d: AcquisitionStartedAt = nil (update err %v), want the claim's start time", i, updateErr)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
	}
}

func TestPgxJobQueue_HeartbeatReleaseAndSettleLeaveACatalogWriteLoadedDuringTheRunValid(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	repo := catalogpersistence.NewPgxTrackRepository(pool)
	ctx := context.Background()

	cases := []struct {
		op  string
		run func(trackID domain.TrackId, fence ports.Fence) error
	}{
		{"heartbeat", func(id domain.TrackId, f ports.Fence) error { return queue.Heartbeat(ctx, id, f, time.Minute) }},
		{"release", func(id domain.TrackId, f ports.Fence) error { return queue.Release(ctx, id, f, time.Hour) }},
		{"settle", func(id domain.TrackId, f ports.Fence) error { return queue.Settle(ctx, id, f) }},
	}
	for _, tc := range cases {
		t.Run(tc.op, func(t *testing.T) {
			track := insertPendingTrack(t, pool, time.Now().Add(-time.Minute))
			job, err := queue.Claim(ctx, time.Minute)
			if err != nil {
				t.Fatalf("Claim = %v", err)
			}
			loaded, err := repo.GetByID(ctx, track.ID, track.UserId)
			if err != nil {
				t.Fatalf("GetByID = %v", err)
			}
			if err := tc.run(track.ID, job.Fence); err != nil {
				t.Fatalf("%s = %v", tc.op, err)
			}
			if err := repo.Update(ctx, loaded, loaded.Version); err != nil {
				t.Errorf("Update loaded before %s = %v, want nil (%s writes only queue columns)", tc.op, err, tc.op)
			}
		})
	}
}

func scheduledAvailableAt(t *testing.T, pool *pgxpool.Pool, trackID domain.TrackId) *time.Time {
	t.Helper()
	var at *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT acquisition_available_at FROM tracks WHERE id = $1`, trackID.UUID()).Scan(&at); err != nil {
		t.Fatalf("select available_at: %v", err)
	}
	return at
}

func TestPgxJobQueue_APanickingReplaceAtTheRunCapIsNotClaimedAgain(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()
	track := insertReadyTrack(t, pool)
	if err := queue.Enqueue(ctx, track.ID, ports.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tracks SET acquisition_run_attempts = 2 WHERE id = $1`, track.ID.UUID()); err != nil {
		t.Fatalf("set run attempts: %v", err)
	}
	acq := &panickingAcquirer{}
	var wg sync.WaitGroup
	scheduler := service.NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1),
		service.WithJobQueue(queue), service.WithPollInterval(5*time.Millisecond))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	deadline := time.Now().Add(5 * time.Second)
	for scheduledAvailableAt(t, pool, track.ID) != nil && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if at := scheduledAvailableAt(t, pool, track.ID); at != nil {
		t.Errorf("available_at = %v after the panic at the run cap, want NULL: the job must leave the queue", at)
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, acqdomain.ErrNoJobAvailable) {
		t.Errorf("Claim after the capped panic = %v, want ErrNoJobAvailable", err)
	}
	if got := acq.runs.Load(); got != 1 {
		t.Errorf("runs = %d, want 1", got)
	}
}

func TestPgxJobQueue_APanickingReplaceBelowTheRunCapIsRetriedAfterItsBackoff(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()
	track := insertReadyTrack(t, pool)
	if err := queue.Enqueue(ctx, track.ID, ports.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	acq := &panickingAcquirer{}
	var wg sync.WaitGroup
	scheduler := service.NewBackgroundAcquisitionScheduler(acq, &wg, make(chan struct{}, 1),
		service.WithJobQueue(queue), service.WithPollInterval(5*time.Millisecond))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	deadline := time.Now().Add(5 * time.Second)
	for acq.runs.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(300 * time.Millisecond)
	scheduler.Shutdown(context.Background())

	at := scheduledAvailableAt(t, pool, track.ID)
	if at == nil {
		t.Fatalf("available_at = NULL after the first panic, want the job kept for a retry")
	}
	if wait := time.Until(*at); wait < 20*time.Second || wait > 31*time.Second {
		t.Errorf("retry scheduled %v from now, want about 30s for the first run", wait)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "ready" {
		t.Errorf("status = %q, want ready", got)
	}
	if got := acq.runs.Load(); got != 1 {
		t.Errorf("runs = %d, want 1", got)
	}
}
