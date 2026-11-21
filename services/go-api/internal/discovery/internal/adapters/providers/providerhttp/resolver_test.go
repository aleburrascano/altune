package providerhttp_test

import (
	"altune/go-api/internal/discovery/internal/adapters/providers/providerhttp"
	"context"
	"errors"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCachedResolver_waiterHonoursOwnDeadline(t *testing.T) {
	release := make(chan struct{})
	r := providerhttp.NewCachedResolver("k", 5*time.Second,
		func(ctx context.Context) (string, time.Time, error) {
			select {
			case <-release:
				return "tok", time.Time{}, nil
			case <-ctx.Done():
				return "", time.Time{}, ctx.Err()
			}
		}, providerhttp.NonEmpty)

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

func newRetryTestResolver() *providerhttp.CachedResolver[string] {
	var n atomic.Int32
	return providerhttp.NewCachedResolver("", 0,
		func(context.Context) (string, time.Time, error) {
			return strings.Repeat("c", int(n.Add(1))), time.Time{}, nil
		}, providerhttp.NonEmpty)
}

func TestWithAuthRetry_authFailureAfterRefreshIsPersistent(t *testing.T) {
	r := newRetryTestResolver()
	_, err := providerhttp.WithAuthRetry(context.Background(), r, func(context.Context, string) (int, int, error) {
		return 0, http.StatusUnauthorized, providerhttp.StatusError{Status: http.StatusUnauthorized}
	})
	var marker persistentAuthMarker
	if !errors.As(err, &marker) || !marker.PersistentAuthFailure() {
		t.Fatalf("err = %v, want a persistent auth failure", err)
	}
}

func TestWithAuthRetry_refreshThatFixesItIsNotAFailure(t *testing.T) {
	r := newRetryTestResolver()
	var calls int
	got, err := providerhttp.WithAuthRetry(context.Background(), r, func(context.Context, string) (int, int, error) {
		calls++
		if calls == 1 {
			return 0, http.StatusUnauthorized, providerhttp.StatusError{Status: http.StatusUnauthorized}
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
	_, err := providerhttp.WithAuthRetry(context.Background(), r, func(context.Context, string) (int, int, error) {
		calls++
		if calls == 1 {
			return 0, http.StatusForbidden, providerhttp.StatusError{Status: http.StatusForbidden}
		}
		return 0, http.StatusInternalServerError, providerhttp.StatusError{Status: http.StatusInternalServerError}
	})
	var marker persistentAuthMarker
	if errors.As(err, &marker) {
		t.Fatalf("err = %v, want no persistent auth marker", err)
	}
}

func TestCachedResolver_staleInvalidateKeepsTheFreshValue(t *testing.T) {
	r := newRetryTestResolver()
	fresh, err := r.Get(context.Background())
	if err != nil || fresh != "c" {
		t.Fatalf("first get = %q, %v; want c, nil", fresh, err)
	}
	r.Invalidate("stale")
	if got, err := r.Get(context.Background()); err != nil || got != fresh {
		t.Fatalf("after a stale invalidate got %q, %v; want the cached %q", got, err, fresh)
	}
	r.Invalidate(fresh)
	if got, err := r.Get(context.Background()); err != nil || got != "cc" {
		t.Fatalf("after invalidating the cached value got %q, %v; want a newly resolved cc", got, err)
	}
}

func TestCachedResolver_concurrentStaleInvalidatesKeepTheFreshValue(t *testing.T) {
	r := newRetryTestResolver()
	fresh, err := r.Get(context.Background())
	if err != nil {
		t.Fatalf("first get: %v", err)
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.Invalidate("stale")
		}()
	}
	wg.Wait()
	if got, err := r.Get(context.Background()); err != nil || got != fresh {
		t.Fatalf("after concurrent stale invalidates got %q, %v; want the cached %q without a new resolve", got, err, fresh)
	}
}
