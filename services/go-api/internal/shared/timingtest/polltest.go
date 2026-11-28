package timingtest

import (
	"context"
	"testing"
	"time"
)

const interval = 5 * time.Millisecond

func Within(within time.Duration, cond func() bool) bool {
	if cond() {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), within)
	defer cancel()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return cond()
		case <-ticker.C:
			if cond() {
				return true
			}
		}
	}
}

func Until(t testing.TB, within time.Duration, what string, cond func() bool) {
	t.Helper()
	if !Within(within, cond) {
		t.Fatalf("timed out after %s waiting for %s", within, what)
	}
}
