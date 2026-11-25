package sharedtest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const acquisitionQueueLockKey int64 = 0x61637175

func ExclusiveAcquisitionQueue(t testing.TB, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire a connection for the acquisition queue lock: %v", err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", acquisitionQueueLockKey); err != nil {
		conn.Release()
		t.Fatalf("lock the acquisition queue: %v", err)
	}
	t.Cleanup(func() { _ = conn.Hijack().Close(context.Background()) })
}
