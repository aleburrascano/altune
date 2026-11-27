package shutdown_test

import (
	"altune/go-api/internal/app/shutdown"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

func lazyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), "postgres://nobody@127.0.0.1:1/none?connect_timeout=1")
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func redisClient(t *testing.T) *goredis.Client {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = client.Close() })
	return client
}

func poolClosed(pool *pgxpool.Pool) bool {
	err := pool.Ping(context.Background())
	return err != nil && strings.Contains(err.Error(), "closed pool")
}

func TestCloseDatastores_ACleanShutdownClosesBothWithoutWarning(t *testing.T) {
	logged := captureLogs(t, slog.LevelDebug)
	pool, client := lazyPool(t), redisClient(t)
	outcomes := []shutdown.Outcome{
		{Component: shutdown.Background, Status: shutdown.Completed},
		{Component: shutdown.LeaderElection, Status: shutdown.Completed},
	}

	shutdown.CloseDatastores(context.Background(), outcomes, shutdown.Datastores{Pool: pool, Redis: client})

	if !poolClosed(pool) {
		t.Error("pool still open after a clean shutdown")
	}
	if err := client.Ping(context.Background()).Err(); !errors.Is(err, goredis.ErrClosed) {
		t.Errorf("redis ping after close = %v, want %v", err, goredis.ErrClosed)
	}
	if got := logged.String(); got != "" {
		t.Errorf("clean shutdown logged %q, want nothing", got)
	}
}

func TestCloseDatastores_UnfinishedComponentsAreNamedInAWarning(t *testing.T) {
	logged := captureLogs(t, slog.LevelDebug)
	outcomes := []shutdown.Outcome{
		{Component: shutdown.EventFeed, Status: shutdown.Completed},
		{Component: shutdown.Scheduler, Status: shutdown.TimedOut},
		{Component: shutdown.LeaderElection, Status: shutdown.Skipped},
	}

	shutdown.CloseDatastores(context.Background(), outcomes, shutdown.Datastores{})

	got := logged.String()
	if want := `level=WARN msg="closing DB/Redis while components are still shutting down" components="acquisition scheduler, leader election"`; !strings.Contains(got, want) {
		t.Errorf("log = %q, want %q", got, want)
	}
}

func TestCloseDatastores_OnlyASkippedLeaderElectionKeepsThePoolOpen(t *testing.T) {
	cases := map[string]struct {
		outcomes []shutdown.Outcome
		keepOpen bool
	}{
		"skipped election":     {[]shutdown.Outcome{{Component: shutdown.LeaderElection, Status: shutdown.Skipped}}, true},
		"timed out election":   {[]shutdown.Outcome{{Component: shutdown.LeaderElection, Status: shutdown.TimedOut}}, false},
		"another skipped step": {[]shutdown.Outcome{{Component: shutdown.Background, Status: shutdown.Skipped}}, false},
		"no outcomes":          {nil, false},
		"completed election":   {[]shutdown.Outcome{{Component: shutdown.LeaderElection, Status: shutdown.Completed}}, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			captureLogs(t, slog.LevelDebug)
			pool := lazyPool(t)

			shutdown.CloseDatastores(context.Background(), tc.outcomes, shutdown.Datastores{Pool: pool})

			if open := !poolClosed(pool); open != tc.keepOpen {
				t.Errorf("pool open = %v, want %v", open, tc.keepOpen)
			}
		})
	}
}

func TestCloseDatastores_ARetainedLeaderLockKeepsThePoolOpenButClosesRedis(t *testing.T) {
	logged := captureLogs(t, slog.LevelDebug)
	pool, client := lazyPool(t), redisClient(t)
	outcomes := []shutdown.Outcome{
		{Component: shutdown.Background, Status: shutdown.TimedOut},
		{Component: shutdown.LeaderElection, Status: shutdown.Skipped},
	}

	shutdown.CloseDatastores(context.Background(), outcomes, shutdown.Datastores{Pool: pool, Redis: client})

	if poolClosed(pool) {
		t.Error("pool closed although the leader lock is retained; closing it would release the lock before the process exits")
	}
	if err := client.Ping(context.Background()).Err(); !errors.Is(err, goredis.ErrClosed) {
		t.Errorf("redis ping after close = %v, want %v", err, goredis.ErrClosed)
	}
	if got := logged.String(); !strings.Contains(got, "leaving DB pool open") {
		t.Errorf("log = %q, want it to say the pool is left open", got)
	}
}

func TestCloseDatastores_ARedisCloseErrorIsLogged(t *testing.T) {
	logged := captureLogs(t, slog.LevelDebug)
	client := redisClient(t)
	if err := client.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}

	shutdown.CloseDatastores(context.Background(), nil, shutdown.Datastores{Redis: client})

	if got := logged.String(); !strings.Contains(got, "redis client close error") {
		t.Errorf("log = %q, want the redis close error", got)
	}
}
