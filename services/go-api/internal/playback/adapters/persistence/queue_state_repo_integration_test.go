//go:build integration

package persistence

import (
	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func testPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(func() { pool.Close() })
	return pool
}

func stateAt(userId shared.UserId, positionMs int64, at time.Time) *domain.QueueState {
	return &domain.QueueState{
		UserId:       userId,
		TrackIds:     []string{},
		NaturalOrder: []string{},
		PositionMs:   positionMs,
		RepeatMode:   domain.RepeatOff,
		UpdatedAt:    at,
	}
}

func TestDeleteForUser_ErasesPersistedRow(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM playback_queue_state WHERE user_id = $1`, userId.UUID())
	})

	if err := repo.Upsert(ctx, stateAt(userId, 42000, time.Now().UTC())); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if got, err := repo.GetForUser(ctx, userId); err != nil || got == nil {
		t.Fatalf("precondition: state must exist before deletion (got=%v err=%v)", got, err)
	}

	if err := repo.DeleteForUser(ctx, userId); err != nil {
		t.Fatalf("DeleteForUser: %v", err)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser after delete: %v", err)
	}
	if got != nil {
		t.Fatalf("row survived DeleteForUser: %+v", got)
	}
}

func TestDeleteForUser_MissingRowIsNotAnError(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)

	if err := repo.DeleteForUser(context.Background(), shared.NewUserId(uuid.New())); err != nil {
		t.Fatalf("deleting a user with no stored state must be a no-op, got: %v", err)
	}
}

func TestUpsert_OlderSnapshotDoesNotClobberNewer(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM playback_queue_state WHERE user_id = $1`, userId.UUID())
	})

	now := time.Now().UTC()
	newer := stateAt(userId, 65000, now)
	older := stateAt(userId, 60000, now.Add(-5*time.Second))

	if err := repo.Upsert(ctx, newer); err != nil {
		t.Fatalf("Upsert(newer): %v", err)
	}
	if err := repo.Upsert(ctx, older); !errors.Is(err, domain.ErrStaleQueueWrite) {
		t.Fatalf("Upsert(older) = %v, want ErrStaleQueueWrite; a rejected write must not report success", err)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if got == nil {
		t.Fatal("GetForUser returned nil after upsert")
	}
	if got.PositionMs != 65000 {
		t.Fatalf("position_ms = %d, want 65000; a delayed older save reverted the newer snapshot", got.PositionMs)
	}
}

func TestUpsert_NewerSnapshotStillApplies(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM playback_queue_state WHERE user_id = $1`, userId.UUID())
	})

	now := time.Now().UTC()
	if err := repo.Upsert(ctx, stateAt(userId, 60000, now.Add(-5*time.Second))); err != nil {
		t.Fatalf("Upsert(older): %v", err)
	}
	if err := repo.Upsert(ctx, stateAt(userId, 65000, now)); err != nil {
		t.Fatalf("Upsert(newer): %v", err)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if got == nil {
		t.Fatal("GetForUser returned nil after upsert")
	}
	if got.PositionMs != 65000 {
		t.Fatalf("position_ms = %d, want 65000; the newer snapshot did not apply", got.PositionMs)
	}
}

// TestUpsert_FastInstanceClockDoesNotRejectLaterSave reproduces the
// wall-clock ordering defect (#1121): save A is handled first by an instance
// whose clock runs 10s fast, save B is handled after it by an accurate one. B
// is the logically newer snapshot, but ordering by each process's own
// time.Now() made A look newer, so B was rejected and the older A won.
func TestUpsert_FastInstanceClockDoesNotRejectLaterSave(t *testing.T) {
	pool := testPool(t)
	repo := NewPgxQueueStateRepository(pool)
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(),
			`DELETE FROM playback_queue_state WHERE user_id = $1`, userId.UUID())
	})

	fastInstance := stateAt(userId, 60000, time.Now().UTC().Add(10*time.Second))
	if err := repo.Upsert(ctx, fastInstance); err != nil {
		t.Fatalf("Upsert(A, fast clock): %v", err)
	}
	accurateInstance := stateAt(userId, 65000, time.Now())
	if err := repo.Upsert(ctx, accurateInstance); err != nil {
		t.Fatalf("Upsert(B, handled after A) = %v, want nil; a skewed instance clock rejected the newer save", err)
	}

	got, err := repo.GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if got == nil || got.PositionMs != 65000 {
		t.Fatalf("stored state = %+v, want position_ms 65000; the older save won", got)
	}
}

// TestUpsert_SaveDelayedInPoolIsStillStale pins the 409 contract for a truly
// stale write: save A is stamped first but waits for a pooled connection while
// the later save B commits. A must be rejected, not land last and clobber B.
func TestUpsert_SaveDelayedInPoolIsStillStale(t *testing.T) {
	wide := testPool(t)
	cfg, err := pgxpool.ParseConfig(os.Getenv("DATABASE_URL"))
	if err != nil {
		t.Fatalf("parse config: %v", err)
	}
	cfg.MaxConns = 1
	narrow, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(narrow.Close)

	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	t.Cleanup(func() {
		_, _ = wide.Exec(context.Background(),
			`DELETE FROM playback_queue_state WHERE user_id = $1`, userId.UUID())
	})

	held, err := narrow.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}

	older := stateAt(userId, 60000, time.Now())
	done := make(chan error, 1)
	go func() { done <- NewPgxQueueStateRepository(narrow).Upsert(ctx, older) }()

	time.Sleep(300 * time.Millisecond)
	newerErr := NewPgxQueueStateRepository(wide).Upsert(ctx, stateAt(userId, 65000, time.Now()))
	held.Release()
	if newerErr != nil {
		t.Fatalf("Upsert(newer): %v", newerErr)
	}

	if err := <-done; !errors.Is(err, domain.ErrStaleQueueWrite) {
		t.Fatalf("Upsert(older, delayed in pool) = %v, want ErrStaleQueueWrite", err)
	}
	got, err := NewPgxQueueStateRepository(wide).GetForUser(ctx, userId)
	if err != nil {
		t.Fatalf("GetForUser: %v", err)
	}
	if got == nil || got.PositionMs != 65000 {
		t.Fatalf("stored state = %+v, want position_ms 65000; the delayed older save clobbered the newer one", got)
	}
}
