package database_test

import (
	"altune/go-api/internal/shared/database"
	"altune/go-api/internal/shared/dbtest"
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
			got := database.WrapUnreadable("op", sentinel, tc.err)
			if errors.Is(got, sentinel) != tc.wantFlag || database.IsUnreadable(tc.err) != tc.wantFlag {
				t.Fatalf("sentinel flag = %v, want %v (%v)", errors.Is(got, sentinel), tc.wantFlag, got)
			}
			if !errors.Is(got, tc.err) {
				t.Fatalf("cause lost: %v", got)
			}
		})
	}
}

const connectBound = 10 * time.Second

func TestNewPool_UnresponsiveHostFailsWithinTheBoundWithNamedError(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	dsn := fmt.Sprintf("postgres://u:p@%s/db?sslmode=disable", ln.Addr().String())

	done := make(chan error, 1)
	go func() {
		pool, err := database.NewPool(context.Background(), dsn, 0)
		if pool != nil {
			pool.Close()
		}
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from unresponsive host, got nil")
		}
		if !strings.Contains(err.Error(), "database unreachable within "+connectBound.String()) {
			t.Fatalf("error should say the database is unreachable and name the bound, got %q", err.Error())
		}
	case <-time.After(connectBound + 5*time.Second):
		t.Fatalf("NewPool hung past %v against an unresponsive host", connectBound+5*time.Second)
	}
}

func TestNewPool_CallerCancellingTheConnectIsNotReportedAsUnreachable(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	dsn := fmt.Sprintf("postgres://u:p@%s/db?sslmode=disable", ln.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	pool, err := database.NewPool(ctx, dsn, 0)
	if pool != nil {
		pool.Close()
	}

	if err == nil || strings.Contains(err.Error(), "unreachable") {
		t.Fatalf("error = %v, want a ping failure that does not blame an unreachable host for the caller's deadline", err)
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("error = %v, want the caller's deadline as the cause", err)
	}
}

func TestNewPool_RefusesAnEmptyURLAndABadURL(t *testing.T) {
	for name, url := range map[string]string{"empty": "", "unparseable": "postgres://u:p@[::1"} {
		t.Run(name, func(t *testing.T) {
			pool, err := database.NewPool(context.Background(), url, 0)
			if err == nil || pool != nil {
				t.Fatalf("NewPool(%q) = %v, %v, want no pool and an error", url, pool, err)
			}
		})
	}
}

func openPool(t *testing.T, maxConns int) *pgxpool.Pool {
	t.Helper()
	pool, err := database.NewPool(context.Background(), dbtest.DatabaseURL(t), maxConns)
	if err != nil {
		t.Fatalf("NewPool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

const defaultMaxConns = 20

func TestNewPool_PinsMaxConnsFromConfiguration(t *testing.T) {
	cases := []struct {
		name     string
		maxConns int
		want     int32
	}{
		{name: "configured", maxConns: 37, want: 37},
		{name: "one", maxConns: 1, want: 1},
		{name: "int32 ceiling", maxConns: math.MaxInt32, want: math.MaxInt32},
		{name: "zero falls back", maxConns: 0, want: defaultMaxConns},
		{name: "negative falls back", maxConns: -1, want: defaultMaxConns},
		{name: "beyond int32 falls back", maxConns: math.MaxInt32 + 1, want: defaultMaxConns},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := openPool(t, tc.maxConns).Config().MaxConns; got != tc.want {
				t.Fatalf("MaxConns = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestNewPool_ReleasesIdleSessionsAfterABurst(t *testing.T) {
	idle := openPool(t, 6).Config().MaxConnIdleTime

	if idle <= 0 || idle > 5*time.Minute {
		t.Fatalf("MaxConnIdleTime = %s, want a release within 5m so a burst does not pin session slots for pgxpool's 30m default", idle)
	}
}

func TestReadPoolStats_ReportsCeilingAndSaturationCounters(t *testing.T) {
	pool, err := pgxpool.New(context.Background(), "postgres://u:p@127.0.0.1:1/db?sslmode=disable&pool_max_conns=11")
	if err != nil {
		t.Fatalf("new pool: %v", err)
	}
	t.Cleanup(pool.Close)

	stats := database.ReadPoolStats(pool)

	if stats.MaxConns != 11 {
		t.Errorf("MaxConns = %d, want 11", stats.MaxConns)
	}
	if stats.AcquiredConns != 0 || stats.TotalConns != 0 {
		t.Errorf("AcquiredConns = %d, TotalConns = %d, want 0 on an unused pool", stats.AcquiredConns, stats.TotalConns)
	}
	if stats.EmptyAcquireCount != 0 {
		t.Errorf("EmptyAcquireCount = %d, want 0 on an unused pool", stats.EmptyAcquireCount)
	}
}

func TestReadPoolStats_CountsAConnectionHeldFromALivePool(t *testing.T) {
	pool := openPool(t, 3)
	conn, err := pool.Acquire(context.Background())
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer conn.Release()

	stats := database.ReadPoolStats(pool)

	if stats.AcquiredConns != 1 || stats.TotalConns < 1 || stats.MaxConns != 3 {
		t.Errorf("stats = %+v, want one acquired of at least one open, under a ceiling of 3", stats)
	}
}

func TestReadPoolStats_NilPoolReadsAsZero(t *testing.T) {
	if stats := database.ReadPoolStats(nil); stats != (database.PoolStats{}) {
		t.Fatalf("ReadPoolStats(nil) = %+v, want zero value", stats)
	}
}

func TestCheckHealth_ReportsALiveAndAClosedPool(t *testing.T) {
	pool := openPool(t, 2)
	if got := database.CheckHealth(context.Background(), pool); !got.OK || got.Err != nil {
		t.Fatalf("CheckHealth on a live pool = %+v, want OK", got)
	}

	pool.Close()

	if got := database.CheckHealth(context.Background(), pool); got.OK || got.Err == nil {
		t.Fatalf("CheckHealth on a closed pool = %+v, want not OK with the cause", got)
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
