package dbtest

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

func BlockedBy(t testing.TB, pool *pgxpool.Pool, holderPID int) int {
	t.Helper()
	var n int
	err := pool.QueryRow(context.Background(),
		`SELECT count(*) FROM pg_stat_activity WHERE pid <> pg_backend_pid() AND $1 = ANY(pg_blocking_pids(pid))`, holderPID).Scan(&n)
	if err != nil {
		t.Fatalf("count backends blocked by backend %d: %v", holderPID, err)
	}
	return n
}
