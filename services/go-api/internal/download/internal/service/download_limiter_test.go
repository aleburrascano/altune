package service_test

import (
	"altune/go-api/internal/download/internal/service"
	"context"
	"errors"
	"testing"
)

func TestDownloadLimiter_AcquireReturnsCtxErrWhenCancelledWhileFull(t *testing.T) {
	limiter := service.MustLimitedTo(1)
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
	limiter := service.MustLimitedTo(1)
	if err := limiter.Acquire(context.Background()); err != nil {
		t.Fatalf("first Acquire: %v", err)
	}
	limiter.Release()

	if err := limiter.Acquire(context.Background()); err != nil {
		t.Fatalf("Acquire after Release: %v", err)
	}
}

func TestDownloadLimiter_UnlimitedNeverBlocks(t *testing.T) {
	limiter := service.Unlimited()

	for range 100 {
		if err := limiter.Acquire(context.Background()); err != nil {
			t.Fatalf("Acquire on an unlimited limiter: %v", err)
		}
	}
	limiter.Release()
}

func TestDownloadLimiter_ZeroValueRefusesToAcquire(t *testing.T) {
	var limiter service.ConcurrencyLimit

	if err := limiter.Acquire(context.Background()); !errors.Is(err, service.ErrLimitNotConfigured) {
		t.Fatalf("zero limiter Acquire = %v, want ErrLimitNotConfigured", err)
	}
	limiter.Release()
}

func TestLimitedTo_RefusesALimitBelowOne(t *testing.T) {
	for _, n := range []int{0, -3} {
		if _, err := service.LimitedTo(n); !errors.Is(err, service.ErrLimitBelowOne) {
			t.Errorf("LimitedTo(%d) error = %v, want ErrLimitBelowOne", n, err)
		}
	}
}
