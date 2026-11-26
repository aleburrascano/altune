package persistence_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func blindingLights(userID ids.UserId) *download.TrackSnapshot {
	return &download.TrackSnapshot{ID: ids.NewTrackId(), UserId: userID, Title: "Blinding Lights", Artist: "The Weeknd", Album: "After Hours"}
}

func insertPendingTrackForUser(t *testing.T, pool *pgxpool.Pool, userID ids.UserId, availableAt time.Time) *download.TrackSnapshot {
	t.Helper()
	track := blindingLights(userID)
	_, err := pool.Exec(context.Background(),
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

func insertFailedTrackForUser(t *testing.T, pool *pgxpool.Pool, userID ids.UserId) ids.TrackId {
	t.Helper()
	track := blindingLights(userID)
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status) VALUES ($1, $2, $3, $4, $5, $6, 'failed')`,
		track.ID.UUID(), track.UserId.UUID(), track.Title, track.Artist, track.Album, uuid.NewString())
	if err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
	})
	return track.ID
}

func insertPendingTrack(t *testing.T, pool *pgxpool.Pool, availableAt time.Time) *download.TrackSnapshot {
	t.Helper()
	return insertPendingTrackForUser(t, pool, ids.NewUserId(uuid.New()), availableAt)
}

func expireLease(t *testing.T, pool *pgxpool.Pool, trackID ids.TrackId) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE tracks SET acquisition_lease_until = now() - interval '1 second' WHERE id = $1`, trackID.UUID()); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
}

func leaseUntil(t *testing.T, pool *pgxpool.Pool, trackID ids.TrackId) time.Time {
	t.Helper()
	var until time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT acquisition_lease_until FROM tracks WHERE id = $1`, trackID.UUID()).Scan(&until); err != nil {
		t.Fatalf("select lease: %v", err)
	}
	return until
}

func TestPostgresJobQueue_ClaimSkipsUnavailableAndLeasedRows(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
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
	if job.ClaimNumber != 1 {
		t.Errorf("Fence = %d, want 1", job.ClaimNumber)
	}

	if _, err := queue.Claim(ctx, 2*time.Minute); !errors.Is(err, download.ErrNoJobAvailable) {
		t.Errorf("second Claim = %v, want ErrNoJobAvailable (track is leased, future one is not yet due)", err)
	}
}

func TestPostgresJobQueue_ClaimReportsHowLongTheJobWaited(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})

	insertPendingTrack(t, pool, time.Now().Add(-90*time.Second))

	job, err := queue.Claim(context.Background(), 2*time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want a job", err)
	}
	if job.Waited < 89*time.Second || job.Waited > 2*time.Minute {
		t.Errorf("Waited = %v, want about 90s", job.Waited)
	}
}

func TestPostgresJobQueue_ConcurrentClaimersNeverDoubleClaim(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	ctx := context.Background()

	const jobs = 12
	tracks := make(map[ids.TrackId]bool, jobs)
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
			queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
			job, err := queue.Claim(ctx, 2*time.Minute)
			if errors.Is(err, download.ErrNoJobAvailable) {
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

func TestPostgresJobQueue_LapsedLeaseIsReclaimed(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))

	first, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("first Claim = %v, want a job", err)
	}
	if first.TrackID != track.ID {
		t.Fatalf("first Claim returned %s, want %s", first.TrackID, track.ID)
	}

	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, download.ErrNoJobAvailable) {
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
	if second.ClaimNumber != 2 {
		t.Errorf("Fence after reclaim = %d, want 2", second.ClaimNumber)
	}
}

func TestPostgresJobQueue_HeartbeatExtendsLeaseAndFailsOnceLapsed(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := queue.Heartbeat(ctx, track.ID, job.ClaimNumber, time.Hour); err != nil {
		t.Fatalf("Heartbeat = %v, want nil", err)
	}
	if until := leaseUntil(t, pool, track.ID); time.Until(until) < 30*time.Minute {
		t.Errorf("lease after an hour-long heartbeat ends in %s, want about an hour", time.Until(until))
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, download.ErrNoJobAvailable) {
		t.Errorf("Claim after heartbeat extended the lease = %v, want ErrNoJobAvailable", err)
	}

	if err := queue.Settle(ctx, track.ID, job.ClaimNumber); err != nil {
		t.Fatalf("Settle = %v, want nil", err)
	}
	if err := queue.Heartbeat(ctx, track.ID, job.ClaimNumber, time.Minute); err == nil {
		t.Error("Heartbeat after Settle cleared the lease = nil error, want an error")
	}
}

func TestPostgresJobQueue_ReleaseSchedulesRetryAtAvailableAt(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := queue.Release(ctx, track.ID, job.ClaimNumber, time.Hour); err != nil {
		t.Fatalf("Release = %v, want nil", err)
	}

	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, download.ErrNoJobAvailable) {
		t.Errorf("Claim before the backoff elapses = %v, want ErrNoJobAvailable", err)
	}

	if err := queue.Release(ctx, track.ID, job.ClaimNumber, 0); err != nil {
		t.Fatalf("Release (immediate retry) = %v, want nil", err)
	}
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Errorf("Claim after immediate retry = %v, want a job", err)
	}
}

func TestPostgresJobQueue_EnqueueMarksPendingAndClaimable(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()

	track := insertFailedTrack(t, pool)

	if err := queue.Enqueue(ctx, track.ID, download.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}

	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim after Enqueue = %v, want a job", err)
	}
	if job.TrackID != track.ID {
		t.Fatalf("Claim returned %s, want the enqueued track %s", job.TrackID, track.ID)
	}
	if job.Kind != download.JobKindReplace {
		t.Errorf("Kind = %q, want %q", job.Kind, download.JobKindReplace)
	}
}

func TestPostgresJobQueue_FairAcrossUsersFavoursTheUserWithNoInFlightJob(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()

	busyUser := ids.NewUserId(uuid.New())
	idleUser := ids.NewUserId(uuid.New())

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

func TestPostgresJobQueue_ZombieHeartbeatFailsAfterReclaimAndLeavesNewOwnerAlone(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
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
	if newOwner.ClaimNumber == zombie.ClaimNumber {
		t.Fatalf("reclaim did not advance the fence: zombie.Fence=%d newOwner.Fence=%d", zombie.ClaimNumber, newOwner.ClaimNumber)
	}

	if err := queue.Heartbeat(ctx, track.ID, zombie.ClaimNumber, time.Minute); !errors.Is(err, download.ErrLeaseLost) {
		t.Errorf("zombie Heartbeat = %v, want ErrLeaseLost", err)
	}
	if err := queue.Release(ctx, track.ID, zombie.ClaimNumber, 0); !errors.Is(err, download.ErrLeaseLost) {
		t.Errorf("zombie Release = %v, want ErrLeaseLost", err)
	}
	if err := queue.Settle(ctx, track.ID, zombie.ClaimNumber); !errors.Is(err, download.ErrLeaseLost) {
		t.Errorf("zombie Settle = %v, want ErrLeaseLost", err)
	}

	if err := queue.Heartbeat(ctx, track.ID, newOwner.ClaimNumber, time.Minute); err != nil {
		t.Errorf("legitimate owner's Heartbeat after zombie calls = %v, want nil (row must be untouched)", err)
	}
	if err := queue.Settle(ctx, track.ID, newOwner.ClaimNumber); err != nil {
		t.Errorf("legitimate owner's Settle after zombie calls = %v, want nil (row must be untouched)", err)
	}
}

func TestPostgresJobQueue_SettleAfterAReEnqueueLeavesThePendingJobClaimable(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	owner, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want a job", err)
	}
	if err := queue.Enqueue(ctx, track.ID, download.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue while leased = %v, want nil", err)
	}
	if err := queue.Settle(ctx, track.ID, owner.ClaimNumber); err != nil {
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

func TestPostgresJobQueue_SettleOfAFinishedJobClearsItsSchedule(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	owner, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want a job", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tracks SET acquisition_status = 'ready' WHERE id = $1`, track.ID.UUID()); err != nil {
		t.Fatalf("mark ready: %v", err)
	}
	if err := queue.Settle(ctx, track.ID, owner.ClaimNumber); err != nil {
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

func TestPostgresJobQueue_EnqueueKeepsALiveLeaseSoNoSecondWorkerClaims(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	owner, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want a job", err)
	}

	if err := queue.Enqueue(ctx, track.ID, download.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue while leased = %v, want nil", err)
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, download.ErrNoJobAvailable) {
		t.Fatalf("Claim while the owner's lease is live = %v, want ErrNoJobAvailable", err)
	}
	if err := queue.Heartbeat(ctx, track.ID, owner.ClaimNumber, time.Minute); err != nil {
		t.Errorf("owner Heartbeat after a re-enqueue = %v, want nil", err)
	}
}

func TestPostgresJobQueue_EnqueueOnLiveLeaseOfDifferentKindConflicts(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := queue.Enqueue(ctx, track.ID, download.JobKindReplace, 0); !errors.Is(err, domain.ErrJobKindConflict) {
		t.Errorf("Enqueue replace over a running acquire = %v, want ErrJobKindConflict", err)
	}
}

func insertReadyTrack(t *testing.T, pool *pgxpool.Pool) *download.TrackSnapshot {
	t.Helper()
	track := blindingLights(ids.NewUserId(uuid.New()))
	_, err := pool.Exec(context.Background(),
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

func markRowFailed(t *testing.T, pool *pgxpool.Pool, trackID ids.TrackId) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE tracks SET acquisition_status = 'failed', failure_reason = 'audio missing', version = version + 1 WHERE id = $1`,
		trackID.UUID()); err != nil {
		t.Fatalf("mark row failed: %v", err)
	}
}

func acquisitionStatus(t *testing.T, pool *pgxpool.Pool, trackID ids.TrackId) string {
	t.Helper()
	var status string
	if err := pool.QueryRow(context.Background(),
		`SELECT acquisition_status FROM tracks WHERE id = $1`, trackID.UUID()).Scan(&status); err != nil {
		t.Fatalf("select status: %v", err)
	}
	return status
}

func TestPostgresJobQueue_ReplaceOfAReadyTrackKeepsItReadyThroughClaimAndSettle(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()

	track := insertReadyTrack(t, pool)

	if err := queue.Enqueue(ctx, track.ID, download.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "ready" {
		t.Fatalf("status after Enqueue = %q, want ready so the old audio keeps streaming", got)
	}

	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want the replace job", err)
	}
	if job.TrackID != track.ID || job.Kind != download.JobKindReplace {
		t.Fatalf("Claim = %+v, want a replace job for %s", job, track.ID)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "ready" {
		t.Errorf("status when claimed = %q, want ready", got)
	}
	if err := queue.Heartbeat(ctx, track.ID, job.ClaimNumber, time.Minute); err != nil {
		t.Errorf("Heartbeat on a ready track's replace job = %v, want nil", err)
	}

	if err := queue.Settle(ctx, track.ID, job.ClaimNumber); err != nil {
		t.Fatalf("Settle = %v", err)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "ready" {
		t.Errorf("status after a failed replace settles = %q, want ready", got)
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, download.ErrNoJobAvailable) {
		t.Errorf("Claim after Settle = %v, want ErrNoJobAvailable", err)
	}
}

func TestPostgresJobQueue_EnqueueOfAcquireOnAReadyTrackLeavesItReady(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})

	track := insertReadyTrack(t, pool)

	if err := queue.Enqueue(context.Background(), track.ID, download.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "ready" {
		t.Errorf("status = %q, want ready", got)
	}
	if _, err := queue.Claim(context.Background(), time.Minute); !errors.Is(err, download.ErrNoJobAvailable) {
		t.Errorf("Claim = %v, want ErrNoJobAvailable", err)
	}
}

func TestPostgresJobQueue_SecondReplaceEnqueueWhileLeasedIsANoop(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()

	track := insertReadyTrack(t, pool)
	markRowFailed(t, pool, track.ID)
	if err := queue.Enqueue(ctx, track.ID, download.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := queue.Enqueue(ctx, track.ID, download.JobKindAcquire, 0); !errors.Is(err, domain.ErrJobKindConflict) {
		t.Errorf("Enqueue of another kind while a replace is leased = %v, want ErrJobKindConflict", err)
	}
}

type panickingAcquirer struct{ runs atomic.Int32 }

func (a *panickingAcquirer) Execute(context.Context, ids.UserId, ids.TrackId) error {
	a.runs.Add(1)
	panic("boom")
}

func (a *panickingAcquirer) ExecuteReplace(ctx context.Context, u ids.UserId, t ids.TrackId) error {
	return a.Execute(ctx, u, t)
}

func (a *panickingAcquirer) RefuseQueued(context.Context, ids.UserId, ids.TrackId) {}

func TestPostgresJobQueue_PanickedJobRowIsNotClaimableAgainImmediately(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	acq := &panickingAcquirer{}
	scheduler := download.MustNewBackgroundAcquisitionScheduler(acq, 1, time.Now, integrationSchedulerPorts(queue),
		service.WithPollInterval(5*time.Millisecond))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	deadline := time.Now().Add(5 * time.Second)
	for acq.runs.Load() == 0 && time.Now().Before(deadline) {
		_ = clock.Sleep(context.Background(), 5*time.Millisecond)
	}
	_ = clock.Sleep(context.Background(), 300*time.Millisecond)

	if got := acq.runs.Load(); got != 1 {
		t.Errorf("runs = %d, want 1 (a panicked job must back off, not be re-claimed)", got)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "pending" {
		t.Errorf("status = %q, want pending until the attempt cap", got)
	}
}

type recordedQueries struct {
	mu   sync.Mutex
	sql  []string
	args [][]any
}

func (r *recordedQueries) TraceQueryStart(ctx context.Context, _ *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sql = append(r.sql, data.SQL)
	r.args = append(r.args, data.Args)
	return ctx
}

func (*recordedQueries) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func TestPostgresJobQueue_ClaimPlanUsesScheduledAvailableAtIndex(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	ctx := context.Background()
	insertPendingTrack(t, pool, time.Now().Add(-time.Minute))
	recorded := &recordedQueries{}
	if _, err := download.MustNewPostgresJobQueue(dbtest.TracedPool(t, recorded), tracksRequeuer{pool: dbtest.Pool(t)}).Claim(ctx, 30*time.Second); err != nil && !errors.Is(err, download.ErrNoJobAvailable) {
		t.Fatalf("Claim = %v", err)
	}
	if len(recorded.sql) != 1 {
		t.Fatalf("Claim ran %d queries, want the one claim query", len(recorded.sql))
	}

	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if _, err := tx.Exec(ctx, `SET LOCAL enable_seqscan = off`); err != nil {
		t.Fatalf("disable seqscan: %v", err)
	}
	rows, err := tx.Query(ctx, "EXPLAIN "+recorded.sql[0], recorded.args[0]...)
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
	if err := rows.Err(); err != nil {
		t.Fatalf("read plan: %v", err)
	}
	if !strings.Contains(plan.String(), "idx_acquisition_jobs_available_at") {
		t.Errorf("claim plan does not use the scheduled available_at index:\n%s", plan.String())
	}
}

func TestPostgresJobQueue_PendingDepthCountsUnleasedJobsAndReportsOldestAge(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
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

func TestPostgresJobQueue_ReEnqueueAfterPriorClaimsStartsANewRunAndAdvancesTheFence(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
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

	if err := queue.Enqueue(ctx, track.ID, download.JobKindAcquire, 0); err != nil {
		t.Fatalf("re-Enqueue = %v", err)
	}
	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim after re-Enqueue = %v", err)
	}

	if job.Run != 1 || job.ClaimNumber != 5 {
		t.Errorf("Run/Fence = %d/%d, want 1/5", job.Run, job.ClaimNumber)
	}
	if err := queue.Settle(ctx, track.ID, stale.ClaimNumber); !errors.Is(err, download.ErrLeaseLost) {
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

func availableAt(t *testing.T, pool *pgxpool.Pool, trackID ids.TrackId) time.Time {
	t.Helper()
	var at time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT acquisition_available_at FROM tracks WHERE id = $1`, trackID.UUID()).Scan(&at); err != nil {
		t.Fatalf("select available_at: %v", err)
	}
	return at
}

func TestPostgresJobQueue_ReleaseDelayIsMeasuredOnTheDatabaseClock(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
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
	if err := queue.Release(ctx, track.ID, job.ClaimNumber, time.Hour); err != nil {
		t.Fatalf("Release = %v, want nil", err)
	}
	after := databaseClock(t, pool)

	got := availableAt(t, pool, track.ID)
	if got.Before(before.Add(time.Hour)) || got.After(after.Add(time.Hour)) {
		t.Errorf("available_at = %v, want within [%v, %v] (database now() plus one hour)", got, before.Add(time.Hour), after.Add(time.Hour))
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, download.ErrNoJobAvailable) {
		t.Errorf("Claim inside the delay = %v, want ErrNoJobAvailable", err)
	}

	before = databaseClock(t, pool)
	if err := queue.Release(ctx, track.ID, job.ClaimNumber, 0); err != nil {
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

func TestPostgresJobQueue_FencedUpdatesWrapErrorsWithTheirOperation(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})

	missing := ids.TrackIdFromUUID(uuid.New())
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
			if !errors.Is(err, download.ErrLeaseLost) {
				t.Fatalf("zero rows = %v, want ErrLeaseLost", err)
			}
			want := tc.op + " acquisition job for track " + missing.String() + ": " + download.ErrLeaseLost.Error()
			if err.Error() != want {
				t.Errorf("zero-rows error = %q, want %q", err.Error(), want)
			}

			err = tc.run(cancelled)
			if err == nil || errors.Is(err, download.ErrLeaseLost) {
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

func TestPostgresJobQueue_AFencedUpdateBlockedOnTheRowGivesUpAfterFiveSeconds(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()
	track := insertPendingTrack(t, pool, time.Now().Add(-time.Minute))
	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}
	if job.TrackID != track.ID {
		t.Fatalf("claimed track %s, want the inserted %s", job.TrackID, track.ID)
	}
	holder, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	defer func() { _ = holder.Rollback(ctx) }()
	if _, err := holder.Exec(ctx, `SELECT 1 FROM tracks WHERE id = $1 FOR UPDATE`, job.TrackID.UUID()); err != nil {
		t.Fatalf("lock the track row: %v", err)
	}

	start := time.Now()
	err = queue.Heartbeat(ctx, job.TrackID, job.ClaimNumber, time.Minute)
	elapsed := time.Since(start)

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Heartbeat on a locked row = %v, want it to give up with a deadline", err)
	}
	if elapsed < 4500*time.Millisecond || elapsed > 10*time.Second {
		t.Errorf("Heartbeat gave up after %s, want about 5s", elapsed)
	}
}

func TestPostgresJobQueue_AnUpgradeKeepsTheTrackReadyWhileItIsQueued(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
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

func TestPostgresJobQueue_AnUpgradeIsNeverQueuedForATrackWithoutAPlayableFile(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
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

func TestPostgresJobQueue_UserAddsKeepPriorityOverUpgrades(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()
	upgrading := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("F"), sourceURL: sourceURL("up")})
	if err := queue.Enqueue(ctx, upgrading, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue upgrade = %v", err)
	}
	added := insertPendingTrack(t, pool, time.Now().Add(-time.Second))

	if _, err := queue.ClaimUpgrade(ctx, time.Minute); !errors.Is(err, download.ErrNoJobAvailable) {
		t.Fatalf("ClaimUpgrade while a user add waits = %v, want ErrNoJobAvailable", err)
	}
	userJob, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v, want the user's add (never the upgrade)", err)
	}
	if userJob.TrackID != added.ID || userJob.Kind != download.JobKindAcquire {
		t.Errorf("Claim returned %+v, want the user add %s", userJob, added.ID)
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, download.ErrNoJobAvailable) {
		t.Fatalf("second Claim = %v, want ErrNoJobAvailable: the upgrade is not a user job", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tracks SET acquisition_status = 'ready' WHERE id = $1`, added.ID.UUID()); err != nil {
		t.Fatalf("mark the add ready: %v", err)
	}
	if err := queue.Settle(ctx, userJob.TrackID, userJob.ClaimNumber); err != nil {
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

func TestPostgresJobQueue_AnUpgradeNeverOverwritesAQueuedUserReplace(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("replaced")})
	if err := queue.Enqueue(ctx, id, download.JobKindReplace, time.Hour); err != nil {
		t.Fatalf("Enqueue replace = %v", err)
	}

	err := queue.Enqueue(ctx, id, ports.JobKindUpgrade, 0)

	if !errors.Is(err, domain.ErrJobKindConflict) {
		t.Fatalf("Enqueue upgrade over a queued replace = %v, want ErrJobKindConflict", err)
	}
	var kind string
	var queued bool
	if err := pool.QueryRow(ctx, `SELECT acquisition_job_kind, acquisition_available_at > now() + interval '30 minutes' FROM tracks WHERE id = $1`, id.UUID()).Scan(&kind, &queued); err != nil {
		t.Fatalf("read job: %v", err)
	}
	if kind != string(download.JobKindReplace) || !queued {
		t.Errorf("kind = %q, queued for later = %v, want the user's replace untouched", kind, queued)
	}
}

func TestPostgresJobQueue_AUserReplaceTakesOverARunningUpgrade(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("running")})
	if err := queue.Enqueue(ctx, id, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue upgrade = %v", err)
	}
	upgrade, err := queue.ClaimUpgrade(ctx, time.Minute)
	if err != nil || upgrade.TrackID != id {
		t.Fatalf("ClaimUpgrade = %+v, %v, want the upgrade of the track", upgrade, err)
	}

	if err := queue.Enqueue(ctx, id, download.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue replace over a running upgrade = %v, want the user's job to win", err)
	}

	if err := queue.Heartbeat(ctx, id, upgrade.ClaimNumber, time.Minute); !errors.Is(err, download.ErrLeaseLost) {
		t.Errorf("upgrade Heartbeat = %v, want ErrLeaseLost so the upgrade stops", err)
	}
	replace, err := queue.Claim(ctx, time.Minute)
	if err != nil || replace.TrackID != id || replace.Kind != download.JobKindReplace {
		t.Errorf("Claim = %+v, %v, want the user's replace right after", replace, err)
	}
}

func TestPostgresJobQueue_AnUpgradeLosesItsLeaseTheMomentAReplaceTakesOver(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()
	id := insertUpgradableTrack(t, pool, readyTrackSpec{evidence: evidenceOf("D"), sourceURL: sourceURL("running")})
	if err := queue.Enqueue(ctx, id, ports.JobKindUpgrade, 0); err != nil {
		t.Fatalf("Enqueue upgrade = %v", err)
	}
	upgrade, err := queue.ClaimUpgrade(ctx, time.Minute)
	if err != nil {
		t.Fatalf("ClaimUpgrade = %v", err)
	}
	if err := queue.VerifyLease(ctx, id, upgrade.ClaimNumber); err != nil {
		t.Fatalf("VerifyLease while the upgrade holds the lease = %v, want nil", err)
	}

	if err := queue.Enqueue(ctx, id, download.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue replace = %v", err)
	}

	if err := queue.VerifyLease(ctx, id, upgrade.ClaimNumber); !errors.Is(err, download.ErrLeaseLost) {
		t.Errorf("VerifyLease after the takeover = %v, want ErrLeaseLost", err)
	}
}

func scheduledAvailableAt(t *testing.T, pool *pgxpool.Pool, trackID ids.TrackId) *time.Time {
	t.Helper()
	var at *time.Time
	if err := pool.QueryRow(context.Background(),
		`SELECT acquisition_available_at FROM tracks WHERE id = $1`, trackID.UUID()).Scan(&at); err != nil {
		t.Fatalf("select available_at: %v", err)
	}
	return at
}

func TestPostgresJobQueue_APanickingReplaceAtTheRunCapIsNotClaimedAgain(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()
	track := insertReadyTrack(t, pool)
	if err := queue.Enqueue(ctx, track.ID, download.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	if _, err := pool.Exec(ctx, `UPDATE tracks SET acquisition_run_attempts = 2 WHERE id = $1`, track.ID.UUID()); err != nil {
		t.Fatalf("set run attempts: %v", err)
	}
	acq := &panickingAcquirer{}
	scheduler := download.MustNewBackgroundAcquisitionScheduler(acq, 1, time.Now, integrationSchedulerPorts(queue),
		service.WithPollInterval(5*time.Millisecond))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	deadline := time.Now().Add(5 * time.Second)
	for scheduledAvailableAt(t, pool, track.ID) != nil && time.Now().Before(deadline) {
		_ = clock.Sleep(context.Background(), 10*time.Millisecond)
	}

	if at := scheduledAvailableAt(t, pool, track.ID); at != nil {
		t.Errorf("available_at = %v after the panic at the run cap, want NULL: the job must leave the queue", at)
	}
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, download.ErrNoJobAvailable) {
		t.Errorf("Claim after the capped panic = %v, want ErrNoJobAvailable", err)
	}
	if got := acq.runs.Load(); got != 1 {
		t.Errorf("runs = %d, want 1", got)
	}
}

func TestPostgresJobQueue_APanickingReplaceBelowTheRunCapIsRetriedAfterItsBackoff(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()
	track := insertReadyTrack(t, pool)
	if err := queue.Enqueue(ctx, track.ID, download.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	acq := &panickingAcquirer{}
	scheduler := download.MustNewBackgroundAcquisitionScheduler(acq, 1, time.Now, integrationSchedulerPorts(queue),
		service.WithPollInterval(5*time.Millisecond))
	t.Cleanup(func() {
		scheduler.Shutdown(context.Background())
	})

	deadline := time.Now().Add(5 * time.Second)
	for acq.runs.Load() == 0 && time.Now().Before(deadline) {
		_ = clock.Sleep(context.Background(), 5*time.Millisecond)
	}
	_ = clock.Sleep(context.Background(), 300*time.Millisecond)
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

func TestPostgresJobQueue_AReplaceReEnqueuedOverALapsedLeaseSurvivesTheStaleSettle(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()
	track := insertReadyTrack(t, pool)
	if err := queue.Enqueue(ctx, track.ID, download.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	stale, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}
	expireLease(t, pool, track.ID)
	if err := queue.Enqueue(ctx, track.ID, download.JobKindReplace, 0); err != nil {
		t.Fatalf("re-Enqueue over the lapsed lease = %v", err)
	}

	if err := queue.Settle(ctx, track.ID, stale.ClaimNumber); !errors.Is(err, download.ErrLeaseLost) {
		t.Errorf("stale Settle = %v, want ErrLeaseLost", err)
	}

	if scheduledAvailableAt(t, pool, track.ID) == nil {
		t.Error("available_at = NULL after the stale Settle, want the re-enqueued replace kept")
	}
	if job, err := queue.Claim(ctx, time.Minute); err != nil || job.TrackID != track.ID {
		t.Errorf("Claim = %+v, %v, want the re-enqueued replace", job, err)
	}
}

func TestPostgresJobQueue_SettledReplaceLeavesNoJobKindBehind(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := download.MustNewPostgresJobQueue(pool, tracksRequeuer{pool: pool})
	ctx := context.Background()
	track := insertReadyTrack(t, pool)
	if err := queue.Enqueue(ctx, track.ID, download.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := queue.Settle(ctx, track.ID, job.ClaimNumber); err != nil {
		t.Fatalf("Settle = %v", err)
	}

	var kind string
	if err := pool.QueryRow(ctx, `SELECT acquisition_job_kind FROM tracks WHERE id = $1`, track.ID.UUID()).Scan(&kind); err != nil {
		t.Fatalf("read kind: %v", err)
	}
	if kind != string(download.JobKindAcquire) {
		t.Errorf("job kind after a settled replace = %q, want %q", kind, download.JobKindAcquire)
	}
}

func integrationSchedulerPorts(queue ports.DownloadQueue) download.SchedulerPorts {
	return download.SchedulerPorts{
		Queue:               download.QueueOf(queue),
		Upgrades:            download.NoUpgradeLane(),
		Notifier:            download.NoopJobNotifier(),
		Outcomes:            download.NoopOutcomeRecorder(),
		Events:              events.NoopPublisher(),
		VerifySkips:         func() uint64 { return 0 },
		FingerprintVerified: func() bool { return false },
		Timer:               clock.NewTimer,
		Ticker:              clock.NewTicker,
	}
}

type tracksRequeuer struct {
	pool *pgxpool.Pool
}

func (r tracksRequeuer) Requeue(ctx context.Context, id ids.TrackId) error {
	if _, err := r.pool.Exec(ctx,
		`UPDATE tracks SET acquisition_status = 'pending', failure_reason = NULL, acquisition_started_at = now(), version = version + 1
		 WHERE id = $1 AND acquisition_status = 'failed'`, id.UUID()); err != nil {
		return fmt.Errorf("requeue track %s: %w", id, err)
	}
	return nil
}
