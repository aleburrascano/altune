package reliability

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"context"
	"log/slog"
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
	p.outcome.Store(int32(outcomeConnecting))
	return p
}

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
	defer func() {
		if rec := recover(); rec != nil {
			slog.ErrorContext(ctx, "reliability: reachability probe panicked", "recover", rec)
		}
	}()
	p.pollOnce(ctx)
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
	p.recordProbe(status, err == nil, finished, finished.Sub(started))
}

func (p *reachPoller) currentOutcome() reachOutcome {
	return reachOutcome(p.outcome.Load())
}

func (p *reachPoller) degradedReason() string {
	return p.currentOutcome().reason()
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

func (p *reachPoller) currentStatus() goapi.Status {
	return p.currentOutcome().status()
}
