package dbtest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	acquisitionQueueLockKey       int64 = 0x61637175
	discographyObservationLockKey int64 = 0x64697363
)

func ExclusiveAcquisitionQueue(t testing.TB, pool *pgxpool.Pool) {
	t.Helper()
	holdAdvisoryLock(t, pool, acquisitionQueueLockKey, "the acquisition queue")
}

func ExclusiveDiscographyObservations(t testing.TB, pool *pgxpool.Pool) {
	t.Helper()
	holdAdvisoryLock(t, pool, discographyObservationLockKey, "the discography observations")
}

func holdAdvisoryLock(t testing.TB, pool *pgxpool.Pool, key int64, what string) {
	t.Helper()
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("acquire a connection for the lock on %s: %v", what, err)
	}
	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", key); err != nil {
		conn.Release()
		t.Fatalf("lock %s: %v", what, err)
	}
	t.Cleanup(func() { _ = conn.Hijack().Close(context.Background()) })
}
