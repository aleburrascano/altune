package runloop

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const loopStartWindow = 100 * time.Millisecond

const shutdownBudget = 2 * time.Second

func countingLoop(starts *atomic.Int64, exited chan<- struct{}) func(context.Context) {
	return func(ctx context.Context) {
		starts.Add(1)
		<-ctx.Done()
		exited <- struct{}{}
	}
}

func TestBackground_PauseResumeGate(t *testing.T) {
	var b Background

	if b.Paused() {
		t.Fatal("a fresh Background must not be paused")
	}

	b.Pause()
	if !b.Paused() {
		t.Fatal("Pause() must engage the gate")
	}

	b.Resume()
	if b.Paused() {
		t.Fatal("Resume() must clear the gate")
	}
}

func TestBackground_SecondSpawnStartsNoSecondLoop(t *testing.T) {
	var b Background
	var starts atomic.Int64
	exited := make(chan struct{}, 2)
	ctx, cancel := context.WithTimeout(context.Background(), shutdownBudget)
	defer cancel()

	b.Spawn(context.Background(), countingLoop(&starts, exited))
	b.Spawn(context.Background(), countingLoop(&starts, exited))
	b.Shutdown(ctx)

	if got := starts.Load(); got != 1 {
		t.Fatalf("two Spawns started %d loops, want 1", got)
	}
	select {
	case <-exited:
	case <-time.After(loopStartWindow):
		t.Fatal("Shutdown returned with a loop still running")
	}
}

func TestBackground_ShutdownBeforeSpawnKeepsTheLoopFromRunning(t *testing.T) {
	var b Background
	var starts atomic.Int64
	exited := make(chan struct{}, 1)
	ctx, cancel := context.WithTimeout(context.Background(), shutdownBudget)
	defer cancel()

	b.Shutdown(ctx)
	b.Spawn(context.Background(), countingLoop(&starts, exited))

	time.Sleep(loopStartWindow)
	if got := starts.Load(); got != 0 {
		t.Fatalf("a loop spawned after Shutdown ran %d times, want 0", got)
	}
}

func TestBackground_ConcurrentSpawnAndShutdownLeavesNoLoopRunning(t *testing.T) {
	var b Background
	var running atomic.Int64
	ctx, cancel := context.WithTimeout(context.Background(), shutdownBudget)
	defer cancel()

	var both sync.WaitGroup
	both.Add(2)
	go func() {
		defer both.Done()
		b.Spawn(context.Background(), func(loopCtx context.Context) {
			running.Add(1)
			<-loopCtx.Done()
			running.Add(-1)
		})
	}()
	go func() {
		defer both.Done()
		b.Shutdown(ctx)
	}()
	both.Wait()

	if got := running.Load(); got != 0 {
		t.Fatalf("%d loops outlived a concurrent Spawn and Shutdown, want 0", got)
	}
}
