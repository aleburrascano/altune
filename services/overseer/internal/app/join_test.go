package app_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/history"
	"context"
	"sync"
	"testing"
	"time"
)

type lateWriterBucket struct {
	*fakeBucket
	series  core.Series
	running sync.WaitGroup
}

func (b *lateWriterBucket) UseSeries(series core.Series) { b.series = series }

func (b *lateWriterBucket) Start(ctx context.Context) {
	b.running.Go(func() {
		<-ctx.Done()
		time.Sleep(20 * time.Millisecond)
		b.series.Record(b.id, "late", core.Point{At: time.Now(), Value: 1})
	})
}

func (b *lateWriterBucket) Wait() { b.running.Wait() }

func TestRunJoinsStartedBucketsBeforeClosingHistory(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	since := time.Now().Add(-time.Minute)

	boot(t, cfg, &lateWriterBucket{fakeBucket: plain("late")}).halt(t)

	store := history.Open(cfg.HistoryPath, history.WithSchemaTimeout(time.Minute))
	t.Cleanup(func() { _ = store.Close() })
	points, err := store.Query("late", "late", since, time.Now().Add(time.Minute))
	if err != nil || len(points) != 1 {
		t.Fatalf("the started bucket's shutdown write = %+v, %v; want 1 point stored before history closed", points, err)
	}
	if failed := logs.named("history.write_failed"); len(failed) != 0 {
		t.Fatalf("a write reached the history store after it closed: %v", failed)
	}
}
