package reliability_test

import (
	"altune/overseer/internal/buckets/reliability"
	"altune/overseer/internal/core"
	"context"
	"testing"
	"time"
)

var _ core.Waiter = (*reliability.Bucket)(nil)

func TestWaitReturnsOnceThePollerHasStopped(t *testing.T) {
	b := reliability.New(reliability.Deps{Interval: time.Hour})
	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)
	cancel()

	joined := make(chan struct{})
	go func() {
		b.Wait()
		close(joined)
	}()
	select {
	case <-joined:
	case <-time.After(2 * time.Second):
		t.Fatal("Wait did not return after the poller's ctx was cancelled")
	}
}
