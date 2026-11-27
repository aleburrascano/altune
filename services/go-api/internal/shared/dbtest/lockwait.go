package dbtest

import (
	"altune/go-api/internal/shared/polltest"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const lockWaitBound = 10 * time.Second

func LockWaiters(t testing.TB, pool *pgxpool.Pool) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_stat_activity
		  WHERE datname = current_database() AND pid <> pg_backend_pid() AND wait_event_type = 'Lock'`).Scan(&n)
	if err != nil {
		t.Fatalf("count backends waiting on a lock: %v", err)
	}
	return n
}

func AwaitLockWaitersBeyond(t testing.TB, pool *pgxpool.Pool, before, extra int, what string) {
	t.Helper()
	polltest.Until(t, lockWaitBound, what, func() bool { return LockWaiters(t, pool) >= before+extra })
}
