package reliability

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/panicguard"
	"context"
	"sync/atomic"
	"time"
)

type reachChecker interface {
	Health(ctx context.Context) (goapi.Health, error)
}

type reachPoller struct {
	checker  reachChecker
	interval time.Duration
	outcome  atomic.Int32
	samples  *core.RingStore
	series   core.Series
	now      func() time.Time
}

type reachOutcome int32

const (
	outcomeConnecting reachOutcome = iota
	outcomeUp
	outcomeDegraded
	outcomeDown
)

func (o reachOutcome) status() goapi.Status {
	switch o {
	case outcomeUp, outcomeDegraded:
		return goapi.StatusUp
	case outcomeDown:
		return goapi.StatusDown
	default:
		return goapi.StatusConnecting
	}
}

func (o reachOutcome) reason() string {
	if o == outcomeDegraded {
		return goapi.ReasonDegraded
	}
	return ""
}

func (o reachOutcome) String() string {
	if o == outcomeDegraded {
		return goapi.ReasonDegraded
	}
	return o.status().String()
}

func newReachPoller(checker reachChecker, interval time.Duration, now func() time.Time) *reachPoller {
	if interval <= 0 {
		interval = defaultPollInterval
	}
	p := &reachPoller{
		checker:  checker,
		interval: interval,
		samples:  core.NewRingStore(pollCapacity),
		series:   core.DiscardSeries{},
		now:      now,
	}
	p.outcome.Store(int32(outcomeConnecting))
	return p
}

func (p *reachPoller) useSeries(s core.Series) { p.series = s }

func (p *reachPoller) ring() *core.RingStore { return p.samples }

func (p *reachPoller) snapshot() []core.Signal { return p.samples.Snapshot() }

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

func (p *reachPoller) safePollOnce(ctx context.Context) {
	panicguard.Contain(ctx, "reliability: reachability probe panicked", func() { p.pollOnce(ctx) })
}

func (p *reachPoller) pollOnce(ctx context.Context) {
	started := p.now()
	h, err := p.checker.Health(ctx)
	finished := p.now()
	outcome := outcomeDown
	switch {
	case err == nil && h.OK():
		outcome = outcomeUp
	case err == nil && h.Degraded():
		outcome = outcomeDegraded
	}
	p.outcome.Store(int32(outcome))
	status := outcome.status()
	p.samples.Add(core.Signal{At: finished.UTC(), Kind: "reach", Text: status.String()})
	p.recordUp(status, finished)
	if err == nil {
		p.recordLatency(finished, finished.Sub(started))
	}
}

func (p *reachPoller) currentOutcome() reachOutcome {
	return reachOutcome(p.outcome.Load())
}

func (p *reachPoller) recordUp(status goapi.Status, at time.Time) {
	up := 0.0
	if status == goapi.StatusUp {
		up = 1
	}
	p.series.Record(bucketID, seriesUp, core.Point{At: at, Value: up})
}

func (p *reachPoller) recordLatency(at time.Time, latency time.Duration) {
	p.series.Record(bucketID, seriesLatencyMS, core.Point{At: at, Value: float64(latency.Microseconds()) / 1000})
}
