package httpapi_test

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/shell/internal/adapters/httpapi"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestSparkCacheReadsTheStoreAtMostOncePerMinute(t *testing.T) {
	var reads atomic.Int64
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := httpapi.NewSparkCache(func() time.Time { return now })
	read := func() ([]core.SparkPoint, error) {
		reads.Add(1)
		return []core.SparkPoint{{At: now, V: 1}}, nil
	}

	const callers = 50
	var wg sync.WaitGroup
	wg.Add(callers)
	for i := 0; i < callers; i++ {
		go func() {
			defer wg.Done()
			c.Load("reliability", read)
		}()
	}
	wg.Wait()

	for i := 0; i < 29; i++ {
		c.Load("reliability", read)
	}

	if got := reads.Load(); got != 1 {
		t.Fatalf("store reads = %d, want 1 for many calls inside one refresh window", got)
	}

	now = now.Add(time.Minute)
	c.Load("reliability", read)
	if got := reads.Load(); got != 2 {
		t.Fatalf("store reads = %d, want 2 after the refresh window elapses", got)
	}
}

func TestSparkCacheServesLastGoodSparkOnAFailedRead(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	c := httpapi.NewSparkCache(func() time.Time { return now })
	good := []core.SparkPoint{{At: now, V: 42}}
	c.Load("cost", func() ([]core.SparkPoint, error) { return good, nil })

	now = now.Add(time.Minute)
	var failReads int
	failing := func() ([]core.SparkPoint, error) {
		failReads++
		return nil, errBoom
	}
	got := c.Load("cost", failing)
	if len(got) != 1 || got[0].V != 42 {
		t.Fatalf("spark = %+v, want the last good spark preserved on a failed read", got)
	}

	now = now.Add(time.Minute)
	_ = c.Load("cost", failing)
	if failReads != 2 {
		t.Fatalf("failing read ran %d times across two refresh windows, want 2 (one per window)", failReads)
	}
}

type sparkCacheError struct{ msg string }

func (e sparkCacheError) Error() string { return e.msg }

var errBoom = sparkCacheError{"store unreachable"}
