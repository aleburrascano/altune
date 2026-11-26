package persistence_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/download/internal/adapters/acquisitiontest"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/testdb"
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
	pool := testdb.Pool(t)
	testdb.ExclusiveAcquisitionQueue(t, pool)
	return pool
}

func insertFailedTrack(t *testing.T, pool *pgxpool.Pool) *download.TrackSnapshot {
	t.Helper()
	track := failedTrack(t)
	_, err := pool.Exec(context.Background(),
		`INSERT INTO tracks (id, user_id, title, artist, album, dedup_key, acquisition_status) VALUES ($1, $2, $3, $4, $5, $6, 'failed')`,
		track.ID.UUID(), track.UserId.UUID(), track.Title, track.Artist, track.Album, uuid.NewString())
	if err != nil {
		t.Fatalf("insert track: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), `DELETE FROM tracks WHERE id = $1`, track.ID.UUID())
	})
	return track
}

func queued() error { return nil }

type contractPgxCooldowns struct {
	*download.PostgresCooldownStore
	pool *pgxpool.Pool
}

func (c contractPgxCooldowns) AddTrack(t *testing.T) ids.TrackId {
	return insertFailedTrack(t, c.pool).ID
}

func TestPostgresCooldownStore_Contract(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	acquisitiontest.CooldownContract(t, func(*testing.T) acquisitiontest.CooldownUnderTest {
		return contractPgxCooldowns{PostgresCooldownStore: download.NewPostgresCooldownStore(pool), pool: pool}
	})
}

func TestPostgresCooldownStore_SurvivesRestart(t *testing.T) {
	testdb.RequireIntegration(t)
	first, second := newPool(t), testdb.Pool(t)
	track := insertFailedTrack(t, first)
	ctx := context.Background()

	if err := download.NewRetryAdmission(download.NewPostgresCooldownStore(first)).Admit(ctx, track, queued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
	err := download.NewRetryAdmission(download.NewPostgresCooldownStore(second)).Admit(ctx, track, queued)
	if !errors.Is(err, download.ErrCooldownActive) {
		t.Errorf("Admit from restarted process = %v, want ErrCooldownActive", err)
	}
}

func TestPostgresCooldownStore_AdmitsAfterWindowAndReleasesOnlyItsOwn(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	store := download.NewPostgresCooldownStore(pool)
	track := insertFailedTrack(t, pool)
	ctx := context.Background()

	old, ok, _, err := store.Reserve(ctx, track.ID, download.CooldownRetry, time.Minute)
	if err != nil || !ok {
		t.Fatalf("first Reserve = %v, %v; want ok", ok, err)
	}
	if _, ok, _, _ := store.Reserve(ctx, track.ID, download.CooldownReacquire, time.Minute); !ok {
		t.Error("reacquire Reserve blocked by a retry admission; kinds must be independent")
	}
	time.Sleep(20 * time.Millisecond)
	newer, ok, _, err := store.Reserve(ctx, track.ID, download.CooldownRetry, 10*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("Reserve after window = %v, %v; want ok", ok, err)
	}
	if err := store.Release(ctx, track.ID, download.CooldownRetry, old); err != nil {
		t.Fatalf("Release stale: %v", err)
	}
	if _, ok, wait, _ := store.Reserve(ctx, track.ID, download.CooldownRetry, time.Minute); ok {
		t.Error("releasing an older reservation removed the newer one")
	} else if wait <= 0 || wait > time.Minute {
		t.Errorf("refused Reserve wait = %v, want 0 < wait <= %v", wait, time.Minute)
	}
	if err := store.Release(ctx, track.ID, download.CooldownRetry, newer); err != nil {
		t.Fatalf("Release newer: %v", err)
	}
	if _, ok, _, _ := store.Reserve(ctx, track.ID, download.CooldownRetry, time.Minute); !ok {
		t.Error("Reserve after releasing the current reservation was refused")
	}
}

func TestPostgresCooldownStore_ConcurrentReservesAdmitOne(t *testing.T) {
	testdb.RequireIntegration(t)
	pool := newPool(t)
	track := insertFailedTrack(t, pool)
	const racers = 16
	var wins atomic.Int32
	var wg sync.WaitGroup
	start := make(chan struct{})
	for range racers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			store := download.NewPostgresCooldownStore(pool)
			_, ok, _, err := store.Reserve(context.Background(), track.ID, download.CooldownRetry, time.Minute)
			if err != nil {
				t.Errorf("Reserve: %v", err)
			}
			if ok {
				wins.Add(1)
			}
		}()
	}
	close(start)
	wg.Wait()
	if got := wins.Load(); got != 1 {
		t.Errorf("concurrent wins = %d, want exactly 1", got)
	}
}
