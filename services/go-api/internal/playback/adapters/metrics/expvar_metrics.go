// Package metrics provides an expvar-backed implementation of playback's
// metrics ports. expvar is stdlib, so it adds no dependency: the counters are
// process-global published integers, surfaced to operators through the
// operator-only GET /admin/metrics/live route (no public /debug/vars handler).
package metrics

import (
	"altune/go-api/internal/playback/ports"
	"expvar"
)

// Published expvar variable names for playback's degradation counters.
const (
	EnrichmentFailuresVar       = "playback_now_playing_enrichment_failures_total"
	CorruptStoredStateVar       = "playback_corrupt_stored_state_total"
	QueueStateOpTimeoutsVar     = "playback_queue_state_op_timeouts_total"
	NowPlayingLookupTimeoutsVar = "playback_now_playing_lookup_timeouts_total"
	QueueStateRateLimitedVar    = "playback_queue_state_rate_limited_total"
	ErasureSweepIdleVar         = "playback_erasure_sweep_idle_total"
	QueueStateErasedVar         = "playback_queue_state_erased_total"

	EnrichmentBreakerRejectionsVar = "playback_now_playing_enrichment_breaker_rejections_total"
	// EnrichmentBreakerOpenVar is a 0/1 gauge, not a counter: the one value an
	// operator can read to answer "is enrichment fast-failing right now".
	EnrichmentBreakerOpenVar = "playback_now_playing_enrichment_breaker_open"
)

// Declared at package scope because expvar.NewInt panics on a duplicate name;
// registering once keeps the adapter safe to construct any number of times.
var (
	enrichmentFailures       = expvar.NewInt(EnrichmentFailuresVar)
	corruptStoredState       = expvar.NewInt(CorruptStoredStateVar)
	queueStateOpTimeouts     = expvar.NewInt(QueueStateOpTimeoutsVar)
	nowPlayingLookupTimeouts = expvar.NewInt(NowPlayingLookupTimeoutsVar)
	queueStateRateLimited    = expvar.NewInt(QueueStateRateLimitedVar)
	erasureSweepIdle         = expvar.NewInt(ErasureSweepIdleVar)
	queueStateErased         = expvar.NewInt(QueueStateErasedVar)

	enrichmentBreakerRejections = expvar.NewInt(EnrichmentBreakerRejectionsVar)
	enrichmentBreakerOpen       = expvar.NewInt(EnrichmentBreakerOpenVar)
)

// The gauge's two edges. A process wires one now-playing reader, so its breaker
// is the gauge's only writer.
const (
	breakerDegraded = 1
	breakerHealthy  = 0
)

// ExpvarPlaybackMetrics implements playback's per-consumer metrics ports by
// incrementing process-global expvar counters.
type ExpvarPlaybackMetrics struct{}

var (
	_ ports.EnrichmentMetrics = ExpvarPlaybackMetrics{}
	_ ports.QueueStateMetrics = ExpvarPlaybackMetrics{}
	_ ports.RateLimitMetrics  = ExpvarPlaybackMetrics{}

	_ ports.ErasureSweepMetrics = ExpvarPlaybackMetrics{}
)

// NewExpvarPlaybackMetrics returns an ExpvarPlaybackMetrics.
func NewExpvarPlaybackMetrics() ExpvarPlaybackMetrics { return ExpvarPlaybackMetrics{} }

func (ExpvarPlaybackMetrics) EnrichmentFailed()         { enrichmentFailures.Add(1) }
func (ExpvarPlaybackMetrics) CorruptStoredState()       { corruptStoredState.Add(1) }
func (ExpvarPlaybackMetrics) QueueStateOpTimedOut()     { queueStateOpTimeouts.Add(1) }
func (ExpvarPlaybackMetrics) NowPlayingLookupTimedOut() { nowPlayingLookupTimeouts.Add(1) }
func (ExpvarPlaybackMetrics) QueueStateRateLimited()    { queueStateRateLimited.Add(1) }

func (ExpvarPlaybackMetrics) SweepIdle()             { erasureSweepIdle.Add(1) }
func (ExpvarPlaybackMetrics) QueueStateErased(n int) { queueStateErased.Add(int64(n)) }

func (ExpvarPlaybackMetrics) EnrichmentBreakerRejected() { enrichmentBreakerRejections.Add(1) }
func (ExpvarPlaybackMetrics) EnrichmentBreakerOpened()   { enrichmentBreakerOpen.Set(breakerDegraded) }
func (ExpvarPlaybackMetrics) EnrichmentBreakerClosed()   { enrichmentBreakerOpen.Set(breakerHealthy) }

// Snapshot is a point-in-time read of the playback degradation counters,
// shaped for JSON exposure on the operator-only GET /admin/metrics/live.
type Snapshot struct {
	EnrichmentFailures       int64 `json:"now_playing_enrichment_failures_total"`
	CorruptStoredState       int64 `json:"corrupt_stored_state_total"`
	QueueStateOpTimeouts     int64 `json:"queue_state_op_timeouts_total"`
	NowPlayingLookupTimeouts int64 `json:"now_playing_lookup_timeouts_total"`
	QueueStateRateLimited    int64 `json:"queue_state_rate_limited_total"`
	ErasureSweepIdle         int64 `json:"erasure_sweep_idle_total"`
	QueueStateErased         int64 `json:"queue_state_erased_total"`

	EnrichmentBreakerRejections int64 `json:"now_playing_enrichment_breaker_rejections_total"`
	EnrichmentBreakerOpen       bool  `json:"now_playing_enrichment_breaker_open"`
}

// ReadSnapshot returns the current values of the published playback counters.
// It reads the package-scope expvar vars directly so callers can expose these
// counters without reaching the raw expvar registry (which also publishes
// process globals like cmdline and memstats).
func ReadSnapshot() Snapshot {
	return Snapshot{
		EnrichmentFailures:       enrichmentFailures.Value(),
		CorruptStoredState:       corruptStoredState.Value(),
		QueueStateOpTimeouts:     queueStateOpTimeouts.Value(),
		NowPlayingLookupTimeouts: nowPlayingLookupTimeouts.Value(),
		QueueStateRateLimited:    queueStateRateLimited.Value(),
		ErasureSweepIdle:         erasureSweepIdle.Value(),
		QueueStateErased:         queueStateErased.Value(),

		EnrichmentBreakerRejections: enrichmentBreakerRejections.Value(),
		EnrichmentBreakerOpen:       enrichmentBreakerOpen.Value() == breakerDegraded,
	}
}
