package providerhttp

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCachedResolver_waiterHonoursOwnDeadline(t *testing.T) {
	release := make(chan struct{})
	r := NewCachedResolver("k", 5*time.Second,
		func(ctx context.Context) (string, time.Time, error) {
			select {
			case <-release:
				return "tok", time.Time{}, nil
			case <-ctx.Done():
				return "", time.Time{}, ctx.Err()
			}
		}, NonEmpty)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.Get(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("get took %v, want prompt return at the caller deadline", d)
	}

	done := make(chan string, 1)
	go func() {
		v, _ := r.Get(context.Background())
		done <- v
	}()
	close(release)
	select {
	case v := <-done:
		if v != "tok" {
			t.Fatalf("second waiter got %q, want tok", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("second waiter never received the value")
	}
}

type persistentAuthMarker interface{ PersistentAuthFailure() bool }

func newRetryTestResolver() *CachedResolver[string] {
	var n atomic.Int32
	return NewCachedResolver("", 0,
		func(context.Context) (string, time.Time, error) {
			return strings.Repeat("c", int(n.Add(1))), time.Time{}, nil
		}, NonEmpty)
}

func TestWithAuthRetry_authFailureAfterRefreshIsPersistent(t *testing.T) {
	r := newRetryTestResolver()
	_, err := WithAuthRetry(context.Background(), r, func(context.Context, string) (int, int, error) {
		return 0, http.StatusUnauthorized, StatusError{Status: http.StatusUnauthorized}
	})
	var marker persistentAuthMarker
	if !errors.As(err, &marker) || !marker.PersistentAuthFailure() {
		t.Fatalf("err = %v, want a persistent auth failure", err)
	}
}

func TestWithAuthRetry_refreshThatFixesItIsNotAFailure(t *testing.T) {
	r := newRetryTestResolver()
	var calls int
	got, err := WithAuthRetry(context.Background(), r, func(context.Context, string) (int, int, error) {
		calls++
		if calls == 1 {
			return 0, http.StatusUnauthorized, StatusError{Status: http.StatusUnauthorized}
		}
		return 7, http.StatusOK, nil
	})
	if err != nil || got != 7 {
		t.Fatalf("got %d, %v; want 7, nil", got, err)
	}
}

func TestWithAuthRetry_nonAuthFailureOnRetryIsNotPersistentAuth(t *testing.T) {
	r := newRetryTestResolver()
	var calls int
	_, err := WithAuthRetry(context.Background(), r, func(context.Context, string) (int, int, error) {
		calls++
		if calls == 1 {
			return 0, http.StatusForbidden, StatusError{Status: http.StatusForbidden}
		}
		return 0, http.StatusInternalServerError, StatusError{Status: http.StatusInternalServerError}
	})
	var marker persistentAuthMarker
	if errors.As(err, &marker) {
		t.Fatalf("err = %v, want no persistent auth marker", err)
	}
}
