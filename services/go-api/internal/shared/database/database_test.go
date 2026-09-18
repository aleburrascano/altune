package database

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// A listener that is never accepted from: the kernel completes the TCP
// handshake but the server never speaks, so the Postgres startup exchange
// blocks forever unless NewPool bounds it.
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

// The pool ceiling must come from configuration, never from the host's CPU
// count (pgx's own default), and an unusable value must land on the documented
// default rather than on whatever int32 conversion produces.
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

// Saturation is only readable if the ceiling and the acquire counters are, so
// ReadPoolStats is asserted against a pool that has never dialled: pgxpool
// connects lazily, which is what keeps this a unit test.
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
