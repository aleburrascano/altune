package persistence_test

import (
	"altune/go-api/internal/download/internal/adapters/persistence"
	"altune/go-api/internal/download/internal/contracttest"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/service"
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

func insertFailedTrack(t *testing.T, pool *pgxpool.Pool) *domain.TrackSnapshot {
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
	*persistence.PostgresCooldownStore
	pool *pgxpool.Pool
}

func (c contractPgxCooldowns) AddTrack(t *testing.T) ids.TrackId {
	return insertFailedTrack(t, c.pool).ID
}

func (c contractPgxCooldowns) PassCooldown(t *testing.T, d time.Duration) {
	t.Helper()
	passCooldown(t, c.pool, d)
}

func passCooldown(t *testing.T, pool *pgxpool.Pool, d time.Duration) {
	t.Helper()
	if _, err := pool.Exec(context.Background(), `UPDATE acquisition_cooldowns SET admitted_at = admitted_at - make_interval(secs => $1)`, d.Seconds()); err != nil {
		t.Fatalf("age the admissions by %v: %v", d, err)
	}
}

func TestPostgresCooldownStore_Contract(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	contracttest.CooldownContract(t, func(*testing.T) contracttest.CooldownUnderTest {
		return contractPgxCooldowns{PostgresCooldownStore: persistence.NewPostgresCooldownStore(pool), pool: pool}
	})
}

func TestPostgresCooldownStore_SurvivesRestart(t *testing.T) {
	dbtest.RequireIntegration(t)
	first := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, first)
	second := dbtest.Pool(t)
	track := insertFailedTrack(t, first)
	ctx := context.Background()

	if err := service.NewRetryAdmission(persistence.NewPostgresCooldownStore(first)).Admit(ctx, track, queued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
	err := service.NewRetryAdmission(persistence.NewPostgresCooldownStore(second)).Admit(ctx, track, queued)
	if !errors.Is(err, domain.ErrCooldownActive) {
		t.Errorf("Admit from restarted process = %v, want ErrCooldownActive", err)
	}
}

func TestPostgresCooldownStore_AdmitsAfterWindowAndReleasesOnlyItsOwn(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
	store := persistence.NewPostgresCooldownStore(pool)
	track := insertFailedTrack(t, pool)
	ctx := context.Background()

	old, ok, _, err := store.Reserve(ctx, track.ID, domain.CooldownRetry, time.Minute)
	if err != nil || !ok {
		t.Fatalf("first Reserve = %v, %v; want ok", ok, err)
	}
	if _, ok, _, _ := store.Reserve(ctx, track.ID, domain.CooldownReacquire, time.Minute); !ok {
		t.Error("reacquire Reserve blocked by a retry admission; kinds must be independent")
	}
	passCooldown(t, pool, 20*time.Millisecond)
	newer, ok, _, err := store.Reserve(ctx, track.ID, domain.CooldownRetry, 10*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("Reserve after window = %v, %v; want ok", ok, err)
	}
	if err := store.Release(ctx, track.ID, domain.CooldownRetry, old); err != nil {
		t.Fatalf("Release stale: %v", err)
	}
	if _, ok, wait, _ := store.Reserve(ctx, track.ID, domain.CooldownRetry, time.Minute); ok {
		t.Error("releasing an older reservation removed the newer one")
	} else if wait <= 0 || wait > time.Minute {
		t.Errorf("refused Reserve wait = %v, want 0 < wait <= %v", wait, time.Minute)
	}
	if err := store.Release(ctx, track.ID, domain.CooldownRetry, newer); err != nil {
		t.Fatalf("Release newer: %v", err)
	}
	if _, ok, _, _ := store.Reserve(ctx, track.ID, domain.CooldownRetry, time.Minute); !ok {
		t.Error("Reserve after releasing the current reservation was refused")
	}
}

func TestPostgresCooldownStore_ConcurrentReservesAdmitOne(t *testing.T) {
	dbtest.RequireIntegration(t)
	pool := dbtest.Pool(t)
	dbtest.ExclusiveAcquisitionQueue(t, pool)
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
			store := persistence.NewPostgresCooldownStore(pool)
			_, ok, _, err := store.Reserve(context.Background(), track.ID, domain.CooldownRetry, time.Minute)
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
