package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/catalog/domain"
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

	if _, err := queue.Claim(ctx, 2*time.Minute); !errors.Is(err, ports.ErrNoJobAvailable) {
		t.Errorf("second Claim = %v, want ErrNoJobAvailable (track is leased, future one is not yet due)", err)
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
			if errors.Is(err, ports.ErrNoJobAvailable) {
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

	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, ports.ErrNoJobAvailable) {
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
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, ports.ErrNoJobAvailable) {
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

	future := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
	if err := queue.Release(ctx, track.ID, job.Fence, future); err != nil {
		t.Fatalf("Release = %v, want nil", err)
	}

	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, ports.ErrNoJobAvailable) {
		t.Errorf("Claim before the backoff elapses = %v, want ErrNoJobAvailable", err)
	}

	if err := queue.Release(ctx, track.ID, job.Fence, time.Now().Add(-time.Second)); err != nil {
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

	if err := queue.Enqueue(ctx, track.ID, ports.JobKindReplace, time.Now().Add(-time.Second)); err != nil {
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

	if err := queue.Heartbeat(ctx, track.ID, zombie.Fence, time.Minute); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("zombie Heartbeat = %v, want ErrLeaseLost", err)
	}
	if err := queue.Release(ctx, track.ID, zombie.Fence, time.Now()); !errors.Is(err, ports.ErrLeaseLost) {
		t.Errorf("zombie Release = %v, want ErrLeaseLost", err)
	}
	if err := queue.Settle(ctx, track.ID, zombie.Fence); !errors.Is(err, ports.ErrLeaseLost) {
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
	if err := queue.Enqueue(ctx, track.ID, ports.JobKindAcquire, time.Now().Add(-time.Second)); err != nil {
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

	if err := queue.Enqueue(ctx, track.ID, ports.JobKindAcquire, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("Enqueue while leased = %v, want nil", err)
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, ports.ErrNoJobAvailable) {
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

	if err := queue.Enqueue(ctx, track.ID, ports.JobKindReplace, time.Now()); !errors.Is(err, ports.ErrJobKindConflict) {
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

	if err := queue.Enqueue(ctx, track.ID, ports.JobKindReplace, time.Now().Add(-time.Second)); err != nil {
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
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, ports.ErrNoJobAvailable) {
		t.Errorf("Claim after Settle = %v, want ErrNoJobAvailable", err)
	}
}

func TestPgxJobQueue_EnqueueOfAcquireOnAReadyTrackStillMarksPending(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)

	track := insertReadyTrack(t, pool)

	if err := queue.Enqueue(context.Background(), track.ID, ports.JobKindAcquire, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "pending" {
		t.Errorf("status = %q, want pending", got)
	}
}

func TestPgxJobQueue_SecondReplaceEnqueueWhileLeasedIsANoop(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertReadyTrack(t, pool)
	if err := queue.Enqueue(ctx, track.ID, ports.JobKindReplace, time.Now().Add(-time.Second)); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := queue.Enqueue(ctx, track.ID, ports.JobKindAcquire, time.Now()); !errors.Is(err, ports.ErrJobKindConflict) {
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
