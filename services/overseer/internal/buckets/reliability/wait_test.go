package reliability

import (
	"altune/overseer/internal/core"
	"context"
	"testing"
	"time"
)

var _ core.Waiter = (*Bucket)(nil)

func TestWaitReturnsOnceThePollerHasStopped(t *testing.T) {
	b := newBucket(&fakeReader{}, &fakeChecker{}, time.Hour)
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
