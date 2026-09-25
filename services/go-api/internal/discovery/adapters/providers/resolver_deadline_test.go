package providers

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestCachedResolver_waiterHonoursOwnDeadline(t *testing.T) {
	release := make(chan struct{})
	r := newCachedResolver("k", 5*time.Second,
		func(ctx context.Context) (string, time.Time, error) {
			select {
			case <-release:
				return "tok", time.Time{}, nil
			case <-ctx.Done():
				return "", time.Time{}, ctx.Err()
			}
		}, nonEmpty)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := r.get(ctx)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want DeadlineExceeded", err)
	}
	if d := time.Since(start); d > time.Second {
		t.Fatalf("get took %v, want prompt return at the caller deadline", d)
	}

	done := make(chan string, 1)
	go func() {
		v, _ := r.get(context.Background())
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
