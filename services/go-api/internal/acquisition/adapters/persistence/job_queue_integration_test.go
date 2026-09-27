package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"errors"
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

	first, err := queue.Claim(ctx, 50*time.Millisecond)
	if err != nil {
		t.Fatalf("first Claim = %v, want a job", err)
	}
	if first.TrackID != track.ID {
		t.Fatalf("first Claim returned %s, want %s", first.TrackID, track.ID)
	}

	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, ports.ErrNoJobAvailable) {
		t.Errorf("Claim while leased = %v, want ErrNoJobAvailable", err)
	}

	time.Sleep(100 * time.Millisecond)

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
	if _, err := queue.Claim(ctx, 100*time.Millisecond); err != nil {
		t.Fatalf("Claim = %v", err)
	}

	if err := queue.Heartbeat(ctx, track.ID, time.Minute); err != nil {
		t.Fatalf("Heartbeat = %v, want nil", err)
	}
	time.Sleep(150 * time.Millisecond)
	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, ports.ErrNoJobAvailable) {
		t.Errorf("Claim after heartbeat extended the lease = %v, want ErrNoJobAvailable", err)
	}

	if err := queue.Settle(ctx, track.ID); err != nil {
		t.Fatalf("Settle = %v, want nil", err)
	}
	if err := queue.Heartbeat(ctx, track.ID, time.Minute); err == nil {
		t.Error("Heartbeat after Settle cleared the lease = nil error, want an error")
	}
}

func TestPgxJobQueue_ReleaseSchedulesRetryAtAvailableAt(t *testing.T) {
	sharedtest.RequireIntegration(t)
	pool := newPool(t)
	queue := NewPgxJobQueue(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Second))
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim = %v", err)
	}

	future := time.Now().Add(time.Hour).UTC().Truncate(time.Millisecond)
	if err := queue.Release(ctx, track.ID, future); err != nil {
		t.Fatalf("Release = %v, want nil", err)
	}

	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, ports.ErrNoJobAvailable) {
		t.Errorf("Claim before the backoff elapses = %v, want ErrNoJobAvailable", err)
	}

	if err := queue.Release(ctx, track.ID, time.Now().Add(-time.Second)); err != nil {
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
