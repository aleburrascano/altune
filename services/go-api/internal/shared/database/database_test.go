package database

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.yaml.in/yaml/v3"
)

func TestWrapUnreadable_MarksMissingOrDeniedStoreWithSentinel(t *testing.T) {
	sentinel := errors.New("identity store unavailable")
	cases := map[string]struct {
		err      error
		wantFlag bool
	}{
		"relation missing":      {&pgconn.PgError{Code: "42P01"}, true},
		"privilege denied":      {&pgconn.PgError{Code: "42501"}, true},
		"other sql state":       {&pgconn.PgError{Code: "23505"}, false},
		"non postgres failure":  {errors.New("boom"), false},
		"wrapped relation miss": {fmt.Errorf("x: %w", &pgconn.PgError{Code: "42P01"}), true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := WrapUnreadable("op", sentinel, tc.err)
			if errors.Is(got, sentinel) != tc.wantFlag || IsUnreadable(tc.err) != tc.wantFlag {
				t.Fatalf("sentinel flag = %v, want %v (%v)", errors.Is(got, sentinel), tc.wantFlag, got)
			}
			if !errors.Is(got, tc.err) {
				t.Fatalf("cause lost: %v", got)
			}
		})
	}
}

func TestNewPool_UnresponsiveHostFailsFastWithNamedError(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	prev := connectTimeout
	connectTimeout = 200 * time.Millisecond
	t.Cleanup(func() { connectTimeout = prev })

	dsn := fmt.Sprintf("postgres://u:p@%s/db?sslmode=disable", ln.Addr().String())

	type result struct{ err error }
	done := make(chan result, 1)
	go func() {
		pool, err := NewPool(context.Background(), dsn, 0)
		if pool != nil {
			pool.Close()
		}
		done <- result{err}
	}()

	select {
	case r := <-done:
		if r.err == nil {
			t.Fatal("expected error from unresponsive host, got nil")
		}
		if !errors.Is(r.err, ErrUnreachable) {
			t.Fatalf("expected ErrUnreachable, got %v", r.err)
		}
		if !strings.Contains(r.err.Error(), "database unreachable within 200ms") {
			t.Fatalf("error should name the bound, got %q", r.err.Error())
		}
	case <-time.After(3 * time.Second):
		t.Fatal("NewPool hung past 3s against an unresponsive host")
	}
}

func TestPoolConfig_PinsMaxConnsFromConfiguration(t *testing.T) {
	cases := []struct {
		name     string
		maxConns int
		want     int32
	}{
		{name: "configured", maxConns: 37, want: 37},
		{name: "zero falls back", maxConns: 0, want: defaultMaxConns},
		{name: "negative falls back", maxConns: -1, want: defaultMaxConns},
		{name: "beyond int32 falls back", maxConns: math.MaxInt32 + 1, want: defaultMaxConns},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := poolConfig("postgres://u:p@localhost:5432/db?sslmode=disable", tc.maxConns)
			if err != nil {
				t.Fatalf("poolConfig: %v", err)
			}
			if cfg.MaxConns != tc.want {
				t.Fatalf("MaxConns = %d, want %d", cfg.MaxConns, tc.want)
			}
		})
	}
}

func TestReadPoolStats_ReportsCeilingAndSaturationCounters(t *testing.T) {
	cfg, err := poolConfig("postgres://u:p@127.0.0.1:1/db?sslmode=disable", 11)
	if err != nil {
		t.Fatalf("poolConfig: %v", err)
	}
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	stats := ReadPoolStats(pool)

	if stats.MaxConns != 11 {
		t.Errorf("MaxConns = %d, want 11", stats.MaxConns)
	}
	if stats.AcquiredConns != 0 {
		t.Errorf("AcquiredConns = %d, want 0 on an unused pool", stats.AcquiredConns)
	}
	if stats.EmptyAcquireCount != 0 {
		t.Errorf("EmptyAcquireCount = %d, want 0 on an unused pool", stats.EmptyAcquireCount)
	}
}

func TestReadPoolStats_NilPoolReadsAsZero(t *testing.T) {
	if stats := ReadPoolStats(nil); stats != (PoolStats{}) {
		t.Fatalf("ReadPoolStats(nil) = %+v, want zero value", stats)
	}
}

const (
	supabaseSessionPoolSize = 15
	sessionsKeptFree        = 3
)

func stagingPoolMaxConns(t *testing.T, service string) int {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "..", "deploy", "compose.staging.yml"))
	if err != nil {
		t.Fatalf("read staging compose: %v", err)
	}
	var compose struct {
		Services map[string]struct {
			Environment []string `yaml:"environment"`
		} `yaml:"services"`
	}
	if err := yaml.Unmarshal(raw, &compose); err != nil {
		t.Fatalf("parse staging compose: %v", err)
	}
	for _, kv := range compose.Services[service].Environment {
		if value, ok := strings.CutPrefix(kv, "DB_POOL_MAX_CONNS="); ok {
			n, err := strconv.Atoi(value)
			if err != nil {
				t.Fatalf("%s DB_POOL_MAX_CONNS=%q: %v", service, value, err)
			}
			return n
		}
	}
	t.Fatalf("%s in compose.staging.yml sets no DB_POOL_MAX_CONNS, so it falls back to the default %d", service, defaultMaxConns)
	return 0
}

func TestStagingCompose_TwoInstancesLeaveSessionSlotsFree(t *testing.T) {
	held := 0
	for _, service := range []string{"go-api-blue", "go-api-green"} {
		held += stagingPoolMaxConns(t, service)
	}

	if free := supabaseSessionPoolSize - held; free < sessionsKeptFree {
		t.Fatalf("blue and green together may hold %d of %d session slots, leaving %d; want at least %d free for migration psql and an operator",
			held, supabaseSessionPoolSize, free, sessionsKeptFree)
	}
}

func TestPoolConfig_ReleasesIdleSessionsAfterABurst(t *testing.T) {
	cfg, err := poolConfig("postgres://u:p@localhost:5432/db?sslmode=disable", 6)
	if err != nil {
		t.Fatalf("poolConfig: %v", err)
	}

	if cfg.MaxConnIdleTime <= 0 || cfg.MaxConnIdleTime > 5*time.Minute {
		t.Fatalf("MaxConnIdleTime = %s, want a release within 5m so a burst does not pin session slots for pgxpool's 30m default", cfg.MaxConnIdleTime)
	}
}

func TestReadOnly_RefusesWritesWhenSessionDefaultIsOff(t *testing.T) {
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	if _, err := pool.Exec(ctx, "SET default_transaction_read_only = off"); err != nil {
		t.Fatalf("drop session default: %v", err)
	}

	err = ReadOnly(ctx, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, "CREATE TABLE readonly_guard_probe (id int)")
		return err
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
		t.Fatalf("CREATE TABLE error = %v, want SQLSTATE 25006", err)
	}

	var got int
	err = ReadOnly(ctx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(ctx, "SELECT 41 + 1").Scan(&got)
	})
	if err != nil || got != 42 {
		t.Fatalf("SELECT = %d, %v, want 42, nil", got, err)
	}
}

func readOnlyProbePool(t *testing.T) (context.Context, *pgxpool.Pool) {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	cfg.MaxConns = 1
	cfg.ConnConfig.RuntimeParams["default_transaction_read_only"] = "off"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return ctx, pool
}

func TestReadOnly_RefusesInsertAndWritingCTEOnAnExistingTable(t *testing.T) {
	ctx, pool := readOnlyProbePool(t)
	if _, err := pool.Exec(ctx, "CREATE TABLE IF NOT EXISTS readonly_probe_rows (id int)"); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DROP TABLE IF EXISTS readonly_probe_rows") })

	for name, stmt := range map[string]string{
		"insert":      "INSERT INTO readonly_probe_rows VALUES (1)",
		"writing CTE": "WITH d AS (INSERT INTO readonly_probe_rows VALUES (2) RETURNING 1) SELECT count(*) FROM d",
		"delete":      "DELETE FROM readonly_probe_rows",
	} {
		t.Run(name, func(t *testing.T) {
			err := ReadOnly(ctx, pool, func(tx pgx.Tx) error {
				_, err := tx.Exec(ctx, stmt)
				return err
			})
			var pgErr *pgconn.PgError
			if !errors.As(err, &pgErr) || pgErr.Code != "25006" {
				t.Fatalf("%s error = %v, want SQLSTATE 25006", name, err)
			}
		})
	}

	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM readonly_probe_rows").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rows after refused writes = %d, %v, want 0, nil", rows, err)
	}
}

func TestReadOnly_FnErrorReachesCallerAndRollsBackTheTransaction(t *testing.T) {
	ctx, pool := readOnlyProbePool(t)
	if _, err := pool.Exec(ctx, "CREATE TEMP TABLE readonly_probe_rollback (id int)"); err != nil {
		t.Fatalf("create temp: %v", err)
	}
	sentinel := errors.New("caller gave up")

	err := ReadOnly(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, "INSERT INTO readonly_probe_rollback VALUES (1)"); err != nil {
			return err
		}
		return sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("ReadOnly error = %v, want the fn's error", err)
	}

	var rows int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM readonly_probe_rollback").Scan(&rows); err != nil || rows != 0 {
		t.Fatalf("rows after fn failed = %d, %v, want 0, nil (rolled back)", rows, err)
	}
}

func TestReadOnly_PanicInFnReachesCallerAndFreesTheConnection(t *testing.T) {
	ctx, pool := readOnlyProbePool(t)

	recovered := func() (r any) {
		defer func() { r = recover() }()
		_ = ReadOnly(ctx, pool, func(tx pgx.Tx) error {
			panic("boom in fn")
		})
		return nil
	}()
	if recovered != "boom in fn" {
		t.Fatalf("recovered = %v, want the fn's panic to reach the caller", recovered)
	}

	acquireCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	var got int
	if err := ReadOnly(acquireCtx, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(acquireCtx, "SELECT 7").Scan(&got)
	}); err != nil || got != 7 {
		t.Fatalf("ReadOnly after a panic = %d, %v, want 7, nil on the pool's only connection", got, err)
	}
}

func TestReadOnly_CancelledContextNeverRunsFn(t *testing.T) {
	ctx, pool := readOnlyProbePool(t)
	cancelled, cancel := context.WithCancel(ctx)
	cancel()

	ran := false
	err := ReadOnly(cancelled, pool, func(tx pgx.Tx) error {
		ran = true
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ReadOnly error = %v, want context.Canceled", err)
	}
	if ran {
		t.Fatal("fn ran on a cancelled context")
	}
}

func TestReadOnly_DeadlineMidQueryReturnsAndFreesTheConnection(t *testing.T) {
	ctx, pool := readOnlyProbePool(t)
	short, cancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := ReadOnly(short, pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(short, "SELECT pg_sleep(10)")
		return err
	})
	if err == nil {
		t.Fatal("ReadOnly returned nil for a query cut off by its deadline")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("ReadOnly took %s past a 200ms deadline", elapsed)
	}

	next, cancelNext := context.WithTimeout(ctx, 5*time.Second)
	defer cancelNext()
	var got int
	if err := ReadOnly(next, pool, func(tx pgx.Tx) error {
		return tx.QueryRow(next, "SELECT 9").Scan(&got)
	}); err != nil || got != 9 {
		t.Fatalf("ReadOnly after a deadline = %d, %v, want 9, nil", got, err)
	}
}
