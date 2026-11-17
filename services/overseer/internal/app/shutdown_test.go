package app_test

import (
	"altune/overseer/internal/core"
	"context"
	"sync"
	"testing"
	"time"
)

const shutdownBudget = 50 * time.Millisecond

func TestRunReturnsWithinTheShutdownBudgetWhenTheTickLoopIgnoresCtxAndBlocks(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 5 * time.Millisecond
	cfg.BucketTimeout = time.Hour
	cfg.ShutdownBudget = shutdownBudget
	entered := make(chan struct{})
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	wedged := &fakeBucket{id: "wedged", collect: func(_ context.Context, call int32) ([]core.Signal, error) {
		if call == 2 {
			close(entered)
			<-release
		}
		return []core.Signal{{Text: "ok"}}, nil
	}}
	r := boot(t, cfg, wedged)
	select {
	case <-entered:
	case <-time.After(settleWait):
		t.Fatal("the wedged bucket never entered its second, ctx-ignoring Collect call")
	}

	if err := r.stop(shutdownBudget + 5*time.Second); err != nil {
		t.Fatalf("Run with a wedged tick loop: %v", err)
	}

	warnings := logs.named("overseer.shutdown.tick_loop_did_not_stop")
	if len(warnings) != 1 {
		t.Fatalf("Run gave up on the wedged tick loop with %d tick_loop_did_not_stop warnings; want 1", len(warnings))
	}
	if got := warnings[0]["budget"]; got != float64(shutdownBudget) {
		t.Errorf("tick_loop_did_not_stop budget = %v, want the configured %d", got, shutdownBudget)
	}
}

func TestAnUnsetBudgetWaitsForAWedgedTickLoopInsteadOfGivingUpAtOnce(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 5 * time.Millisecond
	cfg.BucketTimeout = time.Hour
	entered := make(chan struct{})
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	wedged := &fakeBucket{id: "wedged", collect: func(_ context.Context, call int32) ([]core.Signal, error) {
		if call == 2 {
			close(entered)
			<-release
		}
		return []core.Signal{{Text: "ok"}}, nil
	}}
	r := boot(t, cfg, wedged)
	t.Cleanup(releaseOnce)
	select {
	case <-entered:
	case <-time.After(settleWait):
		t.Fatal("the wedged bucket never entered its second Collect call")
	}

	r.cancel()
	select {
	case <-r.done:
		t.Fatalf("Run returned (%v) at once while its tick loop was wedged; an unset ShutdownBudget must keep the 15s default, not mean zero", r.err)
	case <-time.After(200 * time.Millisecond):
	}
	releaseOnce()
	r.halt(t)

	if got := len(logs.named("overseer.shutdown.tick_loop_did_not_stop")); got != 0 {
		t.Errorf("a tick loop released within the default budget logged %d tick_loop_did_not_stop warnings; want none", got)
	}
}
