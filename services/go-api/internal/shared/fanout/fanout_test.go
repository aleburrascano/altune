package fanout_test

import (
	"altune/go-api/internal/shared/fanout"
	"sync/atomic"
	"testing"
	"time"
)

func TestGroupRunsAllWithinLimit(t *testing.T) {
	const limit, n = 3, 50
	var running, peak, done int32
	started := make(chan struct{}, n)
	release := make(chan struct{})
	g := fanout.New(limit)
	submitted := make(chan struct{})
	go func() {
		defer close(submitted)
		for i := 0; i < n; i++ {
			g.Go(func() {
				cur := atomic.AddInt32(&running, 1)
				for {
					p := atomic.LoadInt32(&peak)
					if cur <= p || atomic.CompareAndSwapInt32(&peak, p, cur) {
						break
					}
				}
				started <- struct{}{}
				<-release
				atomic.AddInt32(&done, 1)
				atomic.AddInt32(&running, -1)
			})
		}
	}()

	for i := 0; i < limit; i++ {
		select {
		case <-started:
		case <-time.After(5 * time.Second):
			t.Fatalf("only %d tasks started, want %d overlapping", i, limit)
		}
	}
	time.Sleep(50 * time.Millisecond)
	if got := atomic.LoadInt32(&running); got != limit {
		t.Fatalf("running = %d while held open, want %d", got, limit)
	}

	close(release)
	<-submitted
	g.Wait()
	if done != n {
		t.Fatalf("done = %d, want %d", done, n)
	}
	if peak != limit {
		t.Fatalf("peak concurrency %d, want %d", peak, limit)
	}
}

func sumOnGroup(g *fanout.Group, values []int64) int64 {
	var total atomic.Int64
	for _, v := range values {
		g.Go(func() { total.Add(v) })
	}
	g.Wait()
	return total.Load()
}

func TestGroupWaitReturnsAfterEveryTaskHasRun(t *testing.T) {
	values := make([]int64, 200)
	var want int64
	for i := range values {
		values[i] = int64(i + 1)
		want += values[i]
	}
	if got := sumOnGroup(fanout.New(4), values); got != want {
		t.Fatalf("sum after Wait = %d, want %d", got, want)
	}
}
