package leader

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const testKey int64 = 8_246_113_907_441_099

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

func awaitLeader(t *testing.T, e *Election, within time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(within)
	for time.Now().Before(deadline) {
		if e.IsLeader() {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestElection_OnlyOneInstanceWins(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	first := NewElection(pool, testKey)
	first.Start(ctx)
	t.Cleanup(func() { first.Shutdown(ctx) })

	if !awaitLeader(t, first, 2*time.Second) {
		t.Fatal("first election never acquired the lock")
	}

	second := NewElection(pool, testKey)
	second.Start(ctx)
	t.Cleanup(func() { second.Shutdown(ctx) })

	if awaitLeader(t, second, 500*time.Millisecond) {
		t.Fatal("second election acquired a lock the first already holds")
	}
}

func TestElection_SuccessorTakesOverAfterShutdown(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	outgoing := NewElection(pool, testKey)
	outgoing.Start(ctx)
	if !awaitLeader(t, outgoing, 2*time.Second) {
		t.Fatal("outgoing election never acquired the lock")
	}

	incoming := NewElection(pool, testKey)
	incoming.interval = 50 * time.Millisecond
	incoming.Start(ctx)
	t.Cleanup(func() { incoming.Shutdown(ctx) })

	if incoming.IsLeader() {
		t.Fatal("incoming election is leader while the outgoing one still holds the lock")
	}

	outgoing.Shutdown(ctx)

	if !awaitLeader(t, incoming, 3*time.Second) {
		t.Fatal("incoming election never took over after the outgoing one released")
	}
}

func TestElection_AwaitUnblocksOnAcquire(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	e := NewElection(pool, testKey)
	e.Start(ctx)
	t.Cleanup(func() { e.Shutdown(ctx) })

	awaitCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if !e.Await(awaitCtx) {
		t.Fatal("Await did not unblock after the lock was acquired")
	}
}

func TestElection_AwaitReturnsFalseWhenNeverLeader(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()

	holder := NewElection(pool, testKey)
	holder.Start(ctx)
	t.Cleanup(func() { holder.Shutdown(ctx) })
	if !awaitLeader(t, holder, 2*time.Second) {
		t.Fatal("holder never acquired the lock")
	}

	standby := NewElection(pool, testKey)
	standby.Start(ctx)
	t.Cleanup(func() { standby.Shutdown(ctx) })

	awaitCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
	defer cancel()

	if standby.Await(awaitCtx) {
		t.Fatal("Await reported leadership the standby never held")
	}
}

func TestElection_ShutdownWithoutStartIsSafe(t *testing.T) {
	e := NewElection(nil, testKey)
	e.Shutdown(context.Background())
}
