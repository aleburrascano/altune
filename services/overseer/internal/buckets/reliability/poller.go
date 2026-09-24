package reliability

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

// reachChecker is the seam the reachability poller calls: go-api's open /health.
// Depending on this one-method interface (not the concrete *goapi.Client) is what
// makes the poll path's independence structural — it holds no field the admin
// mirror path touches — and lets a test drive "app down" deterministically.
type reachChecker interface {
	Health(ctx context.Context) (goapi.Health, error)
}

// reachPoller is the authoritative down-detector: an off-box loop that hits
// go-api's open /health on its own ticker and derives up/down entirely from its
// own probes. It owns all of its state — an atomic status and its own bounded
// ring of outcomes — and never reads the mirror's admin-health path, so "is the
// app up" is never conflated with "the admin API is degraded". This is the
// value-add over a mirror-only view: a health view that reads from the app can't
// tell you the app is down.
type reachPoller struct {
	checker  reachChecker
	interval time.Duration
	status   atomic.Int32 // holds a goapi.Status: connecting / up / down
	samples  core.Store
	series   core.Series
	now      func() time.Time
}

// newReachPoller builds a poller in the initial "connecting" state (no probe has
// run yet). A non-positive interval is clamped to the default so the ticker can
// never be disabled or panic.
func newReachPoller(checker reachChecker, interval time.Duration) *reachPoller {
	if interval <= 0 {
		interval = defaultPollInterval
	}
	p := &reachPoller{
		checker:  checker,
		interval: interval,
		samples:  core.NewRingStore(pollCapacity),
		series:   discardSeries{},
		now:      time.Now,
	}
	p.status.Store(int32(goapi.StatusConnecting))
	return p
}

// run polls once immediately (so the first render after startup already has a
// signal), then on every tick, until ctx is done. It is the single background
// goroutine the poller owns and it returns on ctx cancellation, so nothing leaks
// past the app's lifetime.
func (p *reachPoller) run(ctx context.Context) {
	p.safePollOnce(ctx)
	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.safePollOnce(ctx)
		}
	}
}

// safePollOnce runs one probe with a recover, containing any panic so a single
// misbehaving probe can neither crash the whole process nor kill the detector:
// the ticker survives and the next probe runs. This is the degrade-don't-crash
// invariant on the poller's background goroutine — the same containment the
// liveactivity and usage source pumps hold — kept here because run() is a
// long-lived background goroutine outside safeCollect's recover.
func (p *reachPoller) safePollOnce(ctx context.Context) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "reliability: reachability probe panicked", "recover", rec)
		}
	}()
	p.pollOnce(ctx)
}

// pollOnce performs one reachability probe and records the outcome. go-api being
// unreachable (a SourceDownError), answering non-2xx, or reporting a non-"ok"
// status all count as DOWN: the poll is the detector, so it fails toward down
// rather than optimistically reporting up.
func (p *reachPoller) pollOnce(ctx context.Context) {
	started := p.now()
	h, err := p.checker.Health(ctx)
	finished := p.now()
	status := goapi.StatusDown
	if err == nil && h.OK() {
		status = goapi.StatusUp
	}
	p.status.Store(int32(status))
	p.samples.Add(core.Signal{At: finished.UTC(), Kind: "reach", Text: status.String()})
	p.recordProbe(status, err == nil, finished, finished.Sub(started))
}

func (p *reachPoller) recordProbe(status goapi.Status, answered bool, at time.Time, latency time.Duration) {
	up := 0.0
	if status == goapi.StatusUp {
		up = 1
	}
	p.series.Record(bucketID, seriesUp, core.Point{At: at, Value: up})
	if answered {
		p.series.Record(bucketID, seriesLatencyMS, core.Point{At: at, Value: float64(latency.Microseconds()) / 1000})
	}
}

type discardSeries struct{}

func (discardSeries) Record(string, string, core.Point) {}

func (discardSeries) Query(string, string, time.Time, time.Time) ([]core.Point, error) {
	return nil, nil
}

// currentStatus is the poller's latest reachability verdict, read locklessly via
// the atomic so an HTTP render never contends with the poll goroutine.
func (p *reachPoller) currentStatus() goapi.Status {
	return goapi.Status(p.status.Load())
}
