package downloadfromlibrary_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/testdb"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := testdb.Pool(t)
	testdb.ExclusiveAcquisitionQueue(t, pool)
	return pool
}

type trackRow struct {
	ID     ids.TrackId
	UserId ids.UserId
}

func insertTrack(t *testing.T, pool *pgxpool.Pool, status, audioRef string, availableAt *time.Time) trackRow {
	t.Helper()
	track := trackRow{ID: ids.NewTrackId(), UserId: ids.NewUserId(uuid.New())}
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status, audio_ref, acquisition_available_at)
		 VALUES ($1, $2, 'Blinding Lights', 'The Weeknd', 'After Hours', $3, $4, NULLIF($5, ''), $6)`,
		track.ID.UUID(), track.UserId.UUID(), uuid.NewString(), status, audioRef, availableAt)
	if err != nil {
		t.Fatalf("insert %s track: %v", status, err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
	})
	return track
}

func insertPendingTrack(t *testing.T, pool *pgxpool.Pool, availableAt time.Time) trackRow {
	t.Helper()
	return insertTrack(t, pool, "pending", "", &availableAt)
}

func insertFailedTrack(t *testing.T, pool *pgxpool.Pool) trackRow {
	t.Helper()
	return insertTrack(t, pool, "failed", "", nil)
}

func insertReadyTrack(t *testing.T, pool *pgxpool.Pool) trackRow {
	t.Helper()
	return insertTrack(t, pool, "ready", "audio/old.mp3", nil)
}

func expireLease(t *testing.T, pool *pgxpool.Pool, trackID ids.TrackId) {
	t.Helper()
	if _, err := pool.Exec(context.Background(),
		`UPDATE tracks SET acquisition_lease_until = now() - interval '1 second' WHERE id = $1`, trackID.UUID()); err != nil {
		t.Fatalf("expire lease: %v", err)
	}
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

func TestPostgresJobQueue_EnqueueMakesAStaleCatalogWriteConflict(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	queue := download.NewPostgresJobQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
	ctx := context.Background()

	snap := insertFailedTrack(t, pool)
	loaded, err := repo.GetByID(ctx, snap.ID, snap.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}

	if err := queue.Enqueue(ctx, snap.ID, download.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}

	err = repo.Update(ctx, loaded, loaded.Version)
	if !errors.Is(err, library.ErrTrackVersionConflict) {
		t.Fatalf("stale Update = %v, want ErrTrackVersionConflict", err)
	}
	if got := acquisitionStatus(t, pool, snap.ID); got != "pending" {
		t.Errorf("status = %q, want pending (the stale write must not restore failed)", got)
	}
}

func TestPostgresJobQueue_ClaimMakesAStaleCatalogWriteConflict(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	queue := download.NewPostgresJobQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
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
	if !errors.Is(err, library.ErrTrackVersionConflict) {
		t.Fatalf("stale Update = %v, want ErrTrackVersionConflict", err)
	}
}

func TestPostgresJobQueue_RetryBetweenAudioMissingLoadAndWriteLeavesTheTrackPendingAndClaimable(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	queue := download.NewPostgresJobQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
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
	if err := queue.Enqueue(ctx, track.ID, download.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v, want nil", err)
	}

	if err := repo.Update(ctx, loaded, loaded.Version); !errors.Is(err, library.ErrTrackVersionConflict) {
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

func TestPostgresJobQueue_ReplaceEnqueueMakesAStaleCatalogWriteConflict(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	queue := download.NewPostgresJobQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
	ctx := context.Background()

	track := insertReadyTrack(t, pool)
	loaded, err := repo.GetByID(ctx, track.ID, track.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}
	if err := loaded.MarkFailed("audio missing"); err != nil {
		t.Fatalf("MarkFailed = %v", err)
	}

	if err := queue.Enqueue(ctx, track.ID, download.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue replace = %v, want nil", err)
	}

	if err := repo.Update(ctx, loaded, loaded.Version); !errors.Is(err, library.ErrTrackVersionConflict) {
		t.Fatalf("stale Update after a replace enqueue = %v, want ErrTrackVersionConflict", err)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "ready" {
		t.Errorf("status = %q, want ready", got)
	}
}

func TestPostgresJobQueue_SecondEnqueueMakesAWriterLoadedBetweenTheTwoConflict(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	queue := download.NewPostgresJobQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
	ctx := context.Background()

	snap := insertFailedTrack(t, pool)
	if err := queue.Enqueue(ctx, snap.ID, download.JobKindAcquire, time.Hour); err != nil {
		t.Fatalf("first Enqueue = %v", err)
	}
	loaded, err := repo.GetByID(ctx, snap.ID, snap.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}
	if err := queue.Enqueue(ctx, snap.ID, download.JobKindAcquire, 0); err != nil {
		t.Fatalf("second Enqueue = %v", err)
	}

	if err := repo.Update(ctx, loaded, loaded.Version); !errors.Is(err, library.ErrTrackVersionConflict) {
		t.Fatalf("Update loaded between two enqueues = %v, want ErrTrackVersionConflict", err)
	}
}

func TestPostgresJobQueue_ReclaimOfALapsedLeaseMakesAStaleCatalogWriteConflict(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	queue := download.NewPostgresJobQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
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

	if err := repo.Update(ctx, loaded, loaded.Version); !errors.Is(err, library.ErrTrackVersionConflict) {
		t.Fatalf("Update loaded before the reclaim = %v, want ErrTrackVersionConflict", err)
	}
}

func TestPostgresJobQueue_ClaimOfAReplaceJobMakesAStaleCatalogWriteConflict(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	queue := download.NewPostgresJobQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
	ctx := context.Background()

	track := insertReadyTrack(t, pool)
	if err := queue.Enqueue(ctx, track.ID, download.JobKindReplace, 0); err != nil {
		t.Fatalf("Enqueue replace = %v", err)
	}
	loaded, err := repo.GetByID(ctx, track.ID, track.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}

	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim = %v, want the replace job", err)
	}

	if err := repo.Update(ctx, loaded, loaded.Version); !errors.Is(err, library.ErrTrackVersionConflict) {
		t.Fatalf("Update loaded before the replace claim = %v, want ErrTrackVersionConflict", err)
	}
}

func TestPostgresJobQueue_CatalogWriteThatRereadsAfterEnqueueAndClaimLands(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	queue := download.NewPostgresJobQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
	ctx := context.Background()

	snap := insertFailedTrack(t, pool)
	stale, err := repo.GetByID(ctx, snap.ID, snap.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}
	if err := queue.Enqueue(ctx, snap.ID, download.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim = %v", err)
	}
	if err := repo.Update(ctx, stale, stale.Version); !errors.Is(err, library.ErrTrackVersionConflict) {
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

func TestPostgresJobQueue_EnqueueRacingAStaleCatalogWriteAlwaysEndsPending(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	queue := download.NewPostgresJobQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
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
		go func() {
			defer wg.Done()
			<-start
			enqueueErr = queue.Enqueue(ctx, track.ID, download.JobKindAcquire, 0)
		}()
		go func() { defer wg.Done(); <-start; updateErr = repo.Update(ctx, loaded, loaded.Version) }()
		close(start)
		wg.Wait()

		if enqueueErr != nil {
			t.Fatalf("round %d: Enqueue = %v, want nil", i, enqueueErr)
		}
		if updateErr != nil && !errors.Is(updateErr, library.ErrTrackVersionConflict) {
			t.Fatalf("round %d: Update = %v, want nil or ErrTrackVersionConflict", i, updateErr)
		}
		if got := acquisitionStatus(t, pool, track.ID); got != "pending" {
			t.Fatalf("round %d: status = %q (update err %v), want pending whichever write ran first", i, got, updateErr)
		}
	}
}

func TestPostgresJobQueue_ClaimRacingAStaleCatalogWriteKeepsTheStartTime(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	queue := download.NewPostgresJobQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
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

		if errors.Is(claimErr, download.ErrNoJobAvailable) {
			_, _ = pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
			continue
		}
		if claimErr != nil {
			t.Fatalf("round %d: Claim = %v, want a job or ErrNoJobAvailable", i, claimErr)
		}
		if updateErr != nil && !errors.Is(updateErr, library.ErrTrackVersionConflict) {
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

func TestPostgresJobQueue_HeartbeatReleaseAndSettleLeaveACatalogWriteLoadedDuringTheRunValid(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	queue := download.NewPostgresJobQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
	ctx := context.Background()

	cases := []struct {
		op  string
		run func(trackID ids.TrackId, fence download.ClaimNumber) error
	}{
		{"heartbeat", func(id ids.TrackId, f download.ClaimNumber) error { return queue.Heartbeat(ctx, id, f, time.Minute) }},
		{"release", func(id ids.TrackId, f download.ClaimNumber) error { return queue.Release(ctx, id, f, time.Hour) }},
		{"settle", func(id ids.TrackId, f download.ClaimNumber) error { return queue.Settle(ctx, id, f) }},
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
			if err := tc.run(track.ID, job.ClaimNumber); err != nil {
				t.Fatalf("%s = %v", tc.op, err)
			}
			if err := repo.Update(ctx, loaded, loaded.Version); err != nil {
				t.Errorf("Update loaded before %s = %v, want nil (%s writes only queue columns)", tc.op, err, tc.op)
			}
		})
	}
}
