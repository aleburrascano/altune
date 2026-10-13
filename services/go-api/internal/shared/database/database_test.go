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
