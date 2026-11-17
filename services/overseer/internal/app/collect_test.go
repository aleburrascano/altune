package app_test

import (
	"altune/overseer/internal/core"
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func failing(id string) *fakeBucket {
	return &fakeBucket{id: id, collect: func(context.Context, int32) ([]core.Signal, error) {
		return nil, errors.New("source down")
	}}
}

func panicking(id, where string) *fakeBucket {
	b := &fakeBucket{id: id}
	if where == "collect" {
		b.collect = func(context.Context, int32) ([]core.Signal, error) { panic("collect blew up") }
	} else {
		b.store = func([]core.Signal) { panic("store blew up") }
	}
	return b
}

func wantCounts(t *testing.T, r *running, ok, failed int) {
	t.Helper()
	code, h := r.health(t)
	if code != http.StatusOK || h.Status != "ok" {
		t.Errorf("GET /health = %d %q; want 200 ok", code, h.Status)
	}
	if h.BucketsOK != ok || h.BucketsFailed != failed {
		t.Errorf("GET /health counts = ok:%d failed:%d; want ok:%d failed:%d", h.BucketsOK, h.BucketsFailed, ok, failed)
	}
}

func wantCycleLine(t *testing.T, logs *logBuffer, ok, failed int) {
	t.Helper()
	cycles := logs.named("overseer.collect.cycle")
	if len(cycles) == 0 {
		t.Fatal("the app logged no overseer.collect.cycle heartbeat")
	}
	if cycles[0]["ok"] != float64(ok) || cycles[0]["failed"] != float64(failed) {
		t.Errorf("heartbeat = ok:%v failed:%v; want ok:%d failed:%d", cycles[0]["ok"], cycles[0]["failed"], ok, failed)
	}
}

func TestACycleCountsItsHealthyAndFailedBuckets(t *testing.T) {
	logs := captureLog(t)

	r := boot(t, newConfig(t), plain("healthy"), failing("down"))

	wantCounts(t, r, 1, 1)
	r.halt(t)
	wantCycleLine(t, logs, 1, 1)
}

func TestHealthIsOkWithItsCountsRightAfterTheFirstCycle(t *testing.T) {
	captureLog(t)

	r := boot(t, newConfig(t), plain("healthy"))

	wantCounts(t, r, 1, 0)
	if _, h := r.health(t); h.LastCycle == "" {
		t.Error("GET /health reports no last cycle after one completed")
	}
}

func TestAStorePanicCountsAsAFailedBucketAndSparesItsSibling(t *testing.T) {
	logs := captureLog(t)

	r := boot(t, newConfig(t), panicking("boom", "store"), plain("healthy"))

	wantCounts(t, r, 1, 1)
	r.halt(t)
	wantCycleLine(t, logs, 1, 1)
}

func TestACollectPanicIsContainedAndLoggedOnceAtError(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 5 * time.Millisecond
	boom := panicking("boom", "collect")

	r := boot(t, cfg, boom)
	eventually(t, boom.collectedAtLeast(4), "the panicking bucket was not collected on later ticks; the loop died with it")
	wantCounts(t, r, 0, 1)
	r.halt(t)

	panics := logs.named("overseer.collect.bucket_panic")
	if len(panics) != 1 {
		t.Fatalf("a panicking bucket logged bucket_panic %d times over 3 cycles; want 1", len(panics))
	}
	if panics[0]["level"] != "ERROR" || panics[0]["stage"] != "Collect" {
		t.Errorf("bucket_panic = level %v stage %v; want ERROR Collect", panics[0]["level"], panics[0]["stage"])
	}
	if got := len(logs.named("overseer.collect.source_down")); got != 0 {
		t.Errorf("a panic was re-logged as source_down %d times; a crash must not be downgraded to WARN", got)
	}
	if got := boom.stores.Load(); got != 0 {
		t.Errorf("the panicking bucket was handed signals to Store %d times; a contained panic yields none", got)
	}
}

func TestASustainedOutageLogsOneTransitionNotAFloodPerTick(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 5 * time.Millisecond
	var down atomic.Bool
	down.Store(true)
	goapi := &fakeBucket{id: "goapi", collect: func(context.Context, int32) ([]core.Signal, error) {
		if down.Load() {
			return nil, errors.New("source down")
		}
		return []core.Signal{{Text: "ok"}}, nil
	}}

	r := boot(t, cfg, goapi)
	eventually(t, goapi.collectedAtLeast(6), "the outage did not last 5 cycles")
	down.Store(false)
	eventually(t, goapi.collectedAtLeast(goapi.collects.Load()+2), "no cycle completed after the source recovered")
	r.halt(t)

	if got := len(logs.named("overseer.collect.source_down")); got != 1 {
		t.Errorf("a multi-cycle outage logged source_down %d times; want 1 transition, not a per-tick flood", got)
	}
	if got := len(logs.named("overseer.collect.source_up")); got != 1 {
		t.Errorf("recovery logged source_up %d times; want exactly 1", got)
	}
}

func TestAStalledBucketIsCutOffAtItsDeadlineAndItsSiblingStillStores(t *testing.T) {
	logs := captureLog(t)
	cfg := newConfig(t)
	cfg.BucketTimeout = 50 * time.Millisecond
	cancelled := make(chan struct{})
	stalled := &fakeBucket{id: "a-stalled", collect: func(ctx context.Context, _ int32) ([]core.Signal, error) {
		<-ctx.Done()
		close(cancelled)
		return nil, ctx.Err()
	}}
	healthy := plain("z-healthy")

	r := boot(t, cfg, stalled, healthy)

	if !isClosed(cancelled) {
		t.Error("the stalled bucket's Collect was not cancelled; the per-bucket deadline did not fire")
	}
	if healthy.stores.Load() != 1 {
		t.Error("the bucket queued behind the stalled one never stored; the cycle was starved")
	}
	wantCounts(t, r, 1, 1)
	r.halt(t)
	wantCycleLine(t, logs, 1, 1)
}

func TestHealthGoesStalledWhenTheCollectLoopWedges(t *testing.T) {
	captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 10 * time.Millisecond
	cfg.BucketTimeout = 10 * time.Millisecond
	release := make(chan struct{})
	releaseOnce := sync.OnceFunc(func() { close(release) })
	wedged := &fakeBucket{id: "wedged", collect: func(_ context.Context, call int32) ([]core.Signal, error) {
		if call > 1 {
			<-release
		}
		return []core.Signal{{Text: "ok"}}, nil
	}}

	r := boot(t, cfg, wedged)
	t.Cleanup(releaseOnce)
	eventually(t, func() bool {
		code, h := r.health(t)
		return code == http.StatusServiceUnavailable && h.Status == "collect_stalled"
	}, "GET /health stayed green on a wedged collect loop well past its staleness budget")

	releaseOnce()
	r.halt(t)
}

func TestStartedBackgroundWorkKeepsRunningUntilRunStops(t *testing.T) {
	captureLog(t)
	scheduler := &startedBucket{fakeBucket: plain("scheduler"), stopped: make(chan struct{})}

	r := boot(t, newConfig(t), scheduler)
	eventually(t, func() bool { return scheduler.refreshes.Load() >= 2 },
		"background work fired fewer than 2 refreshes through the Start hook; it froze after one, the #1812 regression")
	r.halt(t)

	select {
	case <-scheduler.stopped:
	case <-time.After(2 * time.Second):
		t.Fatal("background work did not drain within 2s of Run returning; the goroutine leaked past shutdown")
	}
}

func TestABucketWithoutAStartHookStillCollectsEveryTick(t *testing.T) {
	captureLog(t)
	cfg := newConfig(t)
	cfg.TickInterval = 5 * time.Millisecond
	b := plain("plain")

	r := boot(t, cfg, b)

	eventually(t, b.collectedAtLeast(3), "a bucket with no Start hook stopped being collected after start-up")
	wantCounts(t, r, 1, 0)
}

type startedBucket struct {
	*fakeBucket
	refreshes atomic.Int32
	stopped   chan struct{}
}

func (s *startedBucket) Start(ctx context.Context) {
	go func() {
		defer close(s.stopped)
		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.refreshes.Add(1)
			}
		}
	}()
}
