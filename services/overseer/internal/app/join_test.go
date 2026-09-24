package app

import (
	"altune/overseer/internal/core"
	"context"
	"sync"
	"testing"
	"time"
)

type closeAwareHistory struct {
	nopHistory
	mu             sync.Mutex
	closed         bool
	recordsAfterIt int
}

func (h *closeAwareHistory) Record(string, string, core.Point) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		h.recordsAfterIt++
	}
}

func (h *closeAwareHistory) Close() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

type lateWriterBucket struct {
	stubBucket
	series  core.Series
	running *sync.WaitGroup
}

func (b lateWriterBucket) Start(ctx context.Context) {
	b.running.Go(func() {
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		b.series.Record("late", "up", core.Point{At: time.Now(), Value: 1})
	})
}

func (b lateWriterBucket) Wait() { b.running.Wait() }

func TestReleaseHistoryJoinsStartedBucketsBeforeClosing(t *testing.T) {
	store := &closeAwareHistory{}
	reg := core.NewRegistry()
	running := &sync.WaitGroup{}
	reg.Register(lateWriterBucket{stubBucket: stubBucket{id: "late"}, series: store, running: running})
	a := newTestApp(reg, time.Second, time.Second)
	a.history = store
	ctx, cancel := context.WithCancel(context.Background())
	a.startBuckets(ctx)

	a.releaseHistory(cancel, a.startPruner(ctx))
	running.Wait()

	store.mu.Lock()
	defer store.mu.Unlock()
	if store.recordsAfterIt != 0 {
		t.Fatalf("%d record(s) reached the history store after it closed", store.recordsAfterIt)
	}
}
