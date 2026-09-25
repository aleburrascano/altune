package app

import (
	"altune/overseer/internal/core"
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// wedgeAfterFirstBucket answers its first Collect normally — the synchronous pass
// Run makes before the tick loop starts — then, on every call after, blocks forever
// without ever looking at ctx. It is the failure the tick-loop shutdown wait exists
// for: unlike stalledBucket, whose per-tick deadline still reaches it through
// ctx.Done(), this bucket never watches ctx at all, so only a caller giving up on
// it — never the bucket noticing cancellation — can end the wait.
type wedgeAfterFirstBucket struct {
	id      string
	entered chan struct{}
	mu      sync.Mutex
	calls   int
}

func newWedgeAfterFirstBucket(id string) *wedgeAfterFirstBucket {
	return &wedgeAfterFirstBucket{id: id, entered: make(chan struct{})}
}

func (b *wedgeAfterFirstBucket) Meta() core.Meta { return core.Meta{ID: b.id} }

func (b *wedgeAfterFirstBucket) Collect(context.Context) ([]core.Signal, error) {
	b.mu.Lock()
	b.calls++
	first := b.calls == 1
	b.mu.Unlock()
	if first {
		return []core.Signal{{Text: "ok"}}, nil
	}
	close(b.entered)
	select {}
}

func (*wedgeAfterFirstBucket) Store([]core.Signal)     {}
func (*wedgeAfterFirstBucket) Snapshot() core.Snapshot { return core.Snapshot{} }

// TestRunReturnsWithinBudgetWhenTheTickLoopIgnoresCtxAndBlocks is the shutdown-path
// proof for the fix: a bucket that ignores ctx mid-tick must not hang SIGTERM
// forever. Before the fix, Run's shutdown defer awaited the tick loop with no
// limit, so this test hung until the suite's own timeout killed it; the fix bounds
// that wait the same way Shutdown bounds the listener's own drain, so Run returns
// promptly with the wedged tick goroutine left behind rather than joined.
func TestRunReturnsWithinBudgetWhenTheTickLoopIgnoresCtxAndBlocks(t *testing.T) {
	prevBudget := shutdownBudget
	shutdownBudget = 50 * time.Millisecond
	t.Cleanup(func() { shutdownBudget = prevBudget })

	bucket := newWedgeAfterFirstBucket("wedged")
	reg := core.NewRegistry()
	reg.Register(bucket)
	cfg := historyConfig(filepath.Join(t.TempDir(), "history.db"))
	cfg.TickInterval = time.Millisecond
	cfg.BucketTimeout = time.Hour
	a := newApp(cfg, reg)
	captureSlog(t)

	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- a.Run(ctx) }()

	select {
	case <-bucket.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("the wedged bucket never entered its second, ctx-ignoring Collect call")
	}

	cancel()

	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(shutdownBudget + 5*time.Second):
		t.Fatal("Run did not return within the shutdown budget of a wedged tick loop")
	}
}
