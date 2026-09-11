//go:build integration

package persistence

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"altune/go-api/internal/playback/domain"
	"altune/go-api/internal/shared"
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
	if err := repo.Upsert(ctx, older); err != nil {
		t.Fatalf("Upsert(older): %v", err)
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
