package app

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartEveryInstanceTicker_RunsWithoutLeadership(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var runs atomic.Int32
	a := &App{election: &fakeElection{}}
	a.startEveryInstanceTicker(ctx, jobBehavioralRankingRefresh, time.Millisecond, func(context.Context) error {
		runs.Add(1)
		return nil
	})

	waitForAtLeast(t, &runs, 3)
	if _, ok := a.SetJobEnabled(jobBehavioralRankingRefresh, false); !ok {
		t.Fatal("SetJobEnabled must find the behavioral ranking refresh job")
	}
	time.Sleep(30 * time.Millisecond)
	baseline := runs.Load()
	time.Sleep(30 * time.Millisecond)
	if extra := runs.Load() - baseline; extra > 0 {
		t.Fatalf("disabled job kept running: %d extra runs", extra)
	}
}
