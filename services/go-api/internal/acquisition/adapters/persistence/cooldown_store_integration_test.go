//go:build integration

package persistence

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// newPool opens a fresh pool per call, so two pools stand in for two go-api
// processes (a restart, or a second replica) sharing one database.
func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func insertFailedTrack(t *testing.T, pool *pgxpool.Pool) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(shared.NewUserId(uuid.New()), "Blinding Lights", "The Weeknd", "After Hours")
	if err != nil {
		t.Fatalf("new track: %v", err)
	}
	if err := track.MarkFailed("boom"); err != nil {
		t.Fatalf("mark failed: %v", err)
	}
	_, err = pool.Exec(context.Background(),
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

// TestPgxCooldownStore_SurvivesRestart is the #986 regression against a real
// database: an admission made through one process's pool still blocks the same
// track through a second, freshly constructed process.
func TestPgxCooldownStore_SurvivesRestart(t *testing.T) {
	first, second := newPool(t), newPool(t)
	track := insertFailedTrack(t, first)
	ctx := context.Background()

	if err := service.NewRetryAdmission(NewPgxCooldownStore(first)).Admit(ctx, track, queued); err != nil {
		t.Fatalf("first Admit = %v, want nil", err)
	}
	err := service.NewRetryAdmission(NewPgxCooldownStore(second)).Admit(ctx, track, queued)
	if !errors.Is(err, service.ErrCooldownActive) {
		t.Errorf("Admit from restarted process = %v, want ErrCooldownActive", err)
	}
}

func TestPgxCooldownStore_AdmitsAfterWindowAndReleasesOnlyItsOwn(t *testing.T) {
	pool := newPool(t)
	store := NewPgxCooldownStore(pool)
	track := insertFailedTrack(t, pool)
	ctx := context.Background()

	old, ok, err := store.Reserve(ctx, track.ID, ports.CooldownRetry, time.Minute)
	if err != nil || !ok {
		t.Fatalf("first Reserve = %v, %v; want ok", ok, err)
	}
	if _, ok, _ := store.Reserve(ctx, track.ID, ports.CooldownReacquire, time.Minute); !ok {
		t.Error("reacquire Reserve blocked by a retry admission; kinds must be independent")
	}
	time.Sleep(20 * time.Millisecond)
	newer, ok, err := store.Reserve(ctx, track.ID, ports.CooldownRetry, 10*time.Millisecond)
	if err != nil || !ok {
		t.Fatalf("Reserve after window = %v, %v; want ok", ok, err)
	}
	if err := store.Release(ctx, track.ID, ports.CooldownRetry, old); err != nil {
		t.Fatalf("Release stale: %v", err)
	}
	if _, ok, _ := store.Reserve(ctx, track.ID, ports.CooldownRetry, time.Minute); ok {
		t.Error("releasing an older reservation removed the newer one")
	}
	if err := store.Release(ctx, track.ID, ports.CooldownRetry, newer); err != nil {
		t.Fatalf("Release newer: %v", err)
	}
	if _, ok, _ := store.Reserve(ctx, track.ID, ports.CooldownRetry, time.Minute); !ok {
		t.Error("Reserve after releasing the current reservation was refused")
	}
}

// TestPgxCooldownStore_ConcurrentReservesAdmitOne races many processes on one
// track: exactly one reservation may win.
func TestPgxCooldownStore_ConcurrentReservesAdmitOne(t *testing.T) {
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
			store := NewPgxCooldownStore(pool)
			_, ok, err := store.Reserve(context.Background(), track.ID, ports.CooldownRetry, time.Minute)
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
