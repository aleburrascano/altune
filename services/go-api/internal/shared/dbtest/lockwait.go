package dbtest

import (
	"altune/go-api/internal/shared/polltest"
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

const lockWaitBound = 10 * time.Second

func AwaitBlockedBy(t testing.TB, pool *pgxpool.Pool, holderPID, want int, what string) {
	t.Helper()
	polltest.Until(t, lockWaitBound, what, func() bool {
		var n int
		err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND $1 = ANY(pg_blocking_pids(pid))`, holderPID).Scan(&n)
		if err != nil {
			t.Fatalf("count backends blocked by backend %d: %v", holderPID, err)
		}
		return n >= want
	})
}

func AwaitBackendBlocked(t testing.TB, pool *pgxpool.Pool, pid int, what string) {
	t.Helper()
	polltest.Until(t, lockWaitBound, what, func() bool {
		var n int
		err := pool.QueryRow(context.Background(),
			`SELECT count(*) FROM pg_stat_activity WHERE pid = $1 AND wait_event_type = 'Lock'`, pid).Scan(&n)
		if err != nil {
			t.Fatalf("check whether backend %d waits on a lock: %v", pid, err)
		}
		return n == 1
	})
}
