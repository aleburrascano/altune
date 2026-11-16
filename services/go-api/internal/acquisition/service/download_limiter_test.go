package service_test

import (
	"altune/go-api/internal/acquisition"
	"context"
	"errors"
	"testing"
)

func TestDownloadLimiter_AcquireReturnsCtxErrWhenCancelledWhileFull(t *testing.T) {
	limiter := acquisition.NewDownloadLimiter(1)
	if err := limiter.Acquire(context.Background()); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := limiter.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire on full limiter with cancelled ctx = %v, want context.Canceled", err)
	}
}

func TestDownloadLimiter_ReleaseFreesASlot(t *testing.T) {
	limiter := acquisition.NewDownloadLimiter(1)
	if err := limiter.Acquire(context.Background()); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	limiter.Release()

	if err := limiter.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
}

func TestDownloadLimiter_NilIsUnlimited(t *testing.T) {
	var limiter *acquisition.DownloadLimiter
	if err := limiter.Acquire(context.Background()); err != nil {
		t.Fatalf("nil limiter Acquire: %v", err)
	}
	limiter.Release()
}
