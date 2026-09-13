package database

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"
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
		pool, err := NewPool(context.Background(), dsn)
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
