package downloadfromlibrary_test

import (
	"altune/go-api/internal/app/bridges/downloadfromlibrary"
	"altune/go-api/internal/download"
	"altune/go-api/internal/library"
	"altune/go-api/internal/shared/dbtest"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
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
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
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

func TestPostgresJobQueue_ClaimDoesNotMakeACatalogWriteStale(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
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

	if err := repo.Update(ctx, loaded, loaded.Version); err != nil {
		t.Fatalf("Update loaded before a claim = %v, want nil (a claim writes only the job row)", err)
	}
}

func TestPostgresJobQueue_RetryBetweenAudioMissingLoadAndWriteLeavesTheTrackPendingAndClaimable(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
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

func TestPostgresJobQueue_ReplaceEnqueueDoesNotMakeACatalogWriteStale(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
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

	if err := repo.Update(ctx, loaded, loaded.Version); err != nil {
		t.Fatalf("Update after a replace enqueue = %v, want nil (a replace enqueue writes only the job row)", err)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "failed" {
		t.Errorf("status = %q, want failed (the catalog write owns status)", got)
	}
}

func TestPostgresJobQueue_SecondEnqueueDoesNotMakeAWriterLoadedBetweenTheTwoStale(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
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

	if err := repo.Update(ctx, loaded, loaded.Version); err != nil {
		t.Fatalf("Update loaded between two enqueues = %v, want nil", err)
	}
}

func TestPostgresJobQueue_ReclaimOfALapsedLeaseDoesNotMakeACatalogWriteStale(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
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

	if err := repo.Update(ctx, loaded, loaded.Version); err != nil {
		t.Fatalf("Update loaded before the reclaim = %v, want nil", err)
	}
}

func TestPostgresJobQueue_ClaimOfAReplaceJobDoesNotMakeACatalogWriteStale(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
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

	if err := repo.Update(ctx, loaded, loaded.Version); err != nil {
		t.Fatalf("Update loaded before the replace claim = %v, want nil", err)
	}
}

func TestPostgresJobQueue_CatalogWriteThatRereadsAfterEnqueueAndClaimLands(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
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
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
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

func TestPostgresJobQueue_ClaimRacingACatalogWriteCannotTouchTheStartTime(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
	ctx := context.Background()

	for i := 0; i < 40; i++ {
		track := insertPendingTrack(t, pool, time.Now().Add(-time.Minute))
		if _, err := pool.Exec(ctx, `UPDATE tracks SET acquisition_started_at = now() - interval '2 minutes' WHERE id = $1`, track.ID.UUID()); err != nil {
			t.Fatalf("set start time: %v", err)
		}
		loaded, err := repo.GetByID(ctx, track.ID, track.UserId)
		if err != nil {
			t.Fatalf("GetByID = %v", err)
		}
		if loaded.AcquisitionStartedAt == nil {
			t.Fatalf("round %d: a pending track has no start time", i)
		}
		want := *loaded.AcquisitionStartedAt

		var wg sync.WaitGroup
		var claimErr, updateErr error
		start := make(chan struct{})
		wg.Add(2)
		go func() { defer wg.Done(); <-start; _, claimErr = queue.Claim(ctx, time.Minute) }()
		go func() { defer wg.Done(); <-start; updateErr = repo.Update(ctx, loaded, loaded.Version) }()
		close(start)
		wg.Wait()

		if claimErr != nil && !errors.Is(claimErr, download.ErrNoJobAvailable) {
			t.Fatalf("round %d: Claim = %v, want a job or ErrNoJobAvailable", i, claimErr)
		}
		if updateErr != nil {
			t.Fatalf("round %d: Update = %v, want nil (a claim writes only the job row)", i, updateErr)
		}
		reread, err := repo.GetByID(ctx, track.ID, track.UserId)
		if err != nil {
			t.Fatalf("round %d: re-read = %v", i, err)
		}
		if reread.AcquisitionStartedAt == nil || !reread.AcquisitionStartedAt.Equal(want) {
			t.Fatalf("round %d: AcquisitionStartedAt = %v, want %v untouched by the claim", i, reread.AcquisitionStartedAt, want)
		}
		_, _ = pool.Exec(ctx, `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
	}
}

type flakyRequeuer struct {
	inner download.TrackLedgerPort
	fail  atomic.Bool
}

func (f *flakyRequeuer) Requeue(ctx context.Context, id ids.TrackId) error {
	if f.fail.Load() {
		return errors.New("ledger unavailable")
	}
	return f.inner.Requeue(ctx, id)
}

func TestPostgresJobQueue_ARefusedTrackMarkedFailedByTheCatalogIsNotClaimed(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
	repo := library.NewPostgresTrackRepository(pool)
	ctx := context.Background()

	track := insertPendingTrack(t, pool, time.Now().Add(-time.Minute))
	loaded, err := repo.GetByID(ctx, track.ID, track.UserId)
	if err != nil {
		t.Fatalf("GetByID = %v", err)
	}
	if err := loaded.MarkFailed("acquisition_refused"); err != nil {
		t.Fatalf("MarkFailed = %v", err)
	}
	if err := repo.Update(ctx, loaded, loaded.Version); err != nil {
		t.Fatalf("Update = %v", err)
	}

	if _, err := queue.Claim(ctx, time.Minute); !errors.Is(err, download.ErrNoJobAvailable) {
		t.Fatalf("Claim of a track the catalog failed = %v, want ErrNoJobAvailable", err)
	}
	if got := acquisitionStatus(t, pool, track.ID); got != "failed" {
		t.Errorf("status = %q, want failed", got)
	}
}

func jobRowExists(t *testing.T, pool *pgxpool.Pool, id ids.TrackId) bool {
	t.Helper()
	var n int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM acquisition_jobs WHERE track_id = $1`, id.UUID()).Scan(&n); err != nil {
		t.Fatalf("count job rows: %v", err)
	}
	return n == 1
}

func TestPostgresJobQueue_EnqueueWhoseStatusFlipFailsStillQueuesTheJobAndTheClaimConverges(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	ledger := download.TrackLedgerPort(downloadfromlibrary.NewLibraryTracks(library.NewPostgresTrackRepository(pool), time.Now))
	requeuer := &flakyRequeuer{inner: ledger}
	queue := download.MustNewPostgresJobQueue(pool, requeuer)
	ctx := context.Background()

	snap := insertFailedTrack(t, pool)
	requeuer.fail.Store(true)
	if err := queue.Enqueue(ctx, snap.ID, download.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue with a failing status flip = %v, want nil (the job is durable)", err)
	}
	if !jobRowExists(t, pool, snap.ID) {
		t.Fatal("no job row after Enqueue, want the job queued")
	}
	if got := acquisitionStatus(t, pool, snap.ID); got != "failed" {
		t.Fatalf("status = %q, want failed until a claim converges it", got)
	}

	requeuer.fail.Store(false)
	if _, err := queue.Claim(ctx, time.Minute); err != nil {
		t.Fatalf("Claim = %v, want the queued job", err)
	}
	if got := acquisitionStatus(t, pool, snap.ID); got != "pending" {
		t.Errorf("status after the claim = %q, want pending", got)
	}
}

func TestPostgresJobQueue_ClaimWhoseStatusFlipFailsReleasesTheJob(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	ledger := download.TrackLedgerPort(downloadfromlibrary.NewLibraryTracks(library.NewPostgresTrackRepository(pool), time.Now))
	requeuer := &flakyRequeuer{inner: ledger}
	queue := download.MustNewPostgresJobQueue(pool, requeuer)
	ctx := context.Background()

	snap := insertFailedTrack(t, pool)
	requeuer.fail.Store(true)
	if err := queue.Enqueue(ctx, snap.ID, download.JobKindAcquire, 0); err != nil {
		t.Fatalf("Enqueue = %v", err)
	}
	if _, err := queue.Claim(ctx, time.Minute); err == nil || errors.Is(err, download.ErrNoJobAvailable) {
		t.Fatalf("Claim with a failing status flip = %v, want the flip error", err)
	}
	var leased bool
	if err := pool.QueryRow(ctx, `SELECT lease_until IS NOT NULL FROM acquisition_jobs WHERE track_id = $1`, snap.ID.UUID()).Scan(&leased); err != nil {
		t.Fatalf("read lease: %v", err)
	}
	if leased {
		t.Error("the job is still leased after the failed claim, want it released")
	}

	requeuer.fail.Store(false)
	if _, err := pool.Exec(ctx, `UPDATE acquisition_jobs SET available_at = now() - interval '1 second' WHERE track_id = $1`, snap.ID.UUID()); err != nil {
		t.Fatalf("make the job due: %v", err)
	}
	job, err := queue.Claim(ctx, time.Minute)
	if err != nil {
		t.Fatalf("Claim after the outage = %v, want the job", err)
	}
	if job.Run != 1 {
		t.Errorf("Run = %d after claims that never started, want 1 (the retry cap counts only real runs)", job.Run)
	}
}

func TestPostgresJobQueue_EnqueueClearsALeftoverFailureReasonThroughTheLedger(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
	ctx := context.Background()

	cases := []struct {
		name  string
		track func() ids.TrackId
		run   func(ids.TrackId) error
	}{
		{"replace on a ready track", func() ids.TrackId { return insertReadyTrack(t, pool).ID }, func(id ids.TrackId) error { return queue.Enqueue(ctx, id, download.JobKindReplace, 0) }},
		{"acquire on a failed track", func() ids.TrackId { return insertFailedTrack(t, pool).ID }, func(id ids.TrackId) error { return queue.Enqueue(ctx, id, download.JobKindAcquire, 0) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			id := tc.track()
			if _, err := pool.Exec(ctx, `UPDATE tracks SET failure_reason = 'old failure' WHERE id = $1`, id.UUID()); err != nil {
				t.Fatalf("seed a leftover reason: %v", err)
			}
			if err := tc.run(id); err != nil {
				t.Fatalf("Enqueue = %v", err)
			}
			var reason *string
			if err := pool.QueryRow(ctx, `SELECT failure_reason FROM tracks WHERE id = $1`, id.UUID()).Scan(&reason); err != nil {
				t.Fatalf("read reason: %v", err)
			}
			if reason != nil && *reason != "" {
				t.Errorf("failure_reason = %q after the enqueue, want it cleared", *reason)
			}
		})
	}
}

func TestPostgresJobQueue_HeartbeatReleaseAndSettleLeaveACatalogWriteLoadedDuringTheRunValid(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := newPool(t)
	queue := newQueue(pool)
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

func newQueue(pool *pgxpool.Pool) *download.PostgresJobQueue {
	return download.MustNewPostgresJobQueue(pool, downloadfromlibrary.NewLibraryTracks(library.NewPostgresTrackRepository(pool), time.Now))
}
