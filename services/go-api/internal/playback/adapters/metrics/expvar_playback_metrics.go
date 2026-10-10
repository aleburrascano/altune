package metrics

import (
	"altune/go-api/internal/playback/ports"
	"expvar"
)

const (
	EnrichmentFailuresVar          = "playback_now_playing_enrichment_failures_total"
	CorruptStoredStateVar          = "playback_corrupt_stored_state_total"
	QueueStateOpTimeoutsVar        = "playback_queue_state_op_timeouts_total"
	QueueStateTransientFailuresVar = "playback_queue_state_op_transient_failures_total"
	QueueStateStaleWritesVar       = "playback_queue_state_stale_writes_total"
	NowPlayingLookupTimeoutsVar    = "playback_now_playing_lookup_timeouts_total"
	QueueStateRateLimitedVar       = "playback_queue_state_rate_limited_total"
	ErasureSweepIdleVar            = "playback_erasure_sweep_idle_total"
	QueueStateErasedVar            = "playback_queue_state_erased_total"
	ErasureOwnerScanTimeoutsVar    = "playback_erasure_owner_scan_timeouts_total"
	ErasureRetryOwnersDroppedVar   = "playback_erasure_retry_owners_dropped_total"

	EnrichmentBreakerRejectionsVar = "playback_now_playing_enrichment_breaker_rejections_total"
	EnrichmentBreakerOpenVar       = "playback_now_playing_enrichment_breaker_open"
)

var (
	enrichmentFailures          = expvar.NewInt(EnrichmentFailuresVar)
	corruptStoredState          = expvar.NewInt(CorruptStoredStateVar)
	queueStateOpTimeouts        = expvar.NewInt(QueueStateOpTimeoutsVar)
	queueStateTransientFailures = expvar.NewInt(QueueStateTransientFailuresVar)
	queueStateStaleWrites       = expvar.NewInt(QueueStateStaleWritesVar)
	nowPlayingLookupTimeouts    = expvar.NewInt(NowPlayingLookupTimeoutsVar)
	queueStateRateLimited       = expvar.NewInt(QueueStateRateLimitedVar)
	erasureSweepIdle            = expvar.NewInt(ErasureSweepIdleVar)
	queueStateErased            = expvar.NewInt(QueueStateErasedVar)
	erasureOwnerScanTimeouts    = expvar.NewInt(ErasureOwnerScanTimeoutsVar)
	erasureRetryOwnersDropped   = expvar.NewInt(ErasureRetryOwnersDroppedVar)

	enrichmentBreakerRejections = expvar.NewInt(EnrichmentBreakerRejectionsVar)
	enrichmentBreakerOpen       = expvar.NewInt(EnrichmentBreakerOpenVar)
)

const (
	breakerDegraded = 1
	breakerHealthy  = 0
)

type ExpvarPlaybackMetrics struct{}

var (
	_ ports.EnrichmentMetrics = ExpvarPlaybackMetrics{}
	_ ports.QueueStateMetrics = ExpvarPlaybackMetrics{}
	_ ports.RateLimitMetrics  = ExpvarPlaybackMetrics{}

	_ ports.ErasureSweepMetrics = ExpvarPlaybackMetrics{}
)

func NewExpvarPlaybackMetrics() ExpvarPlaybackMetrics { return ExpvarPlaybackMetrics{} }

func (ExpvarPlaybackMetrics) EnrichmentFailed()            { enrichmentFailures.Add(1) }
func (ExpvarPlaybackMetrics) CorruptStoredState()          { corruptStoredState.Add(1) }
func (ExpvarPlaybackMetrics) QueueStateOpTimedOut()        { queueStateOpTimeouts.Add(1) }
func (ExpvarPlaybackMetrics) QueueStateOpFailedTransient() { queueStateTransientFailures.Add(1) }
func (ExpvarPlaybackMetrics) QueueStateStaleWrite()        { queueStateStaleWrites.Add(1) }
func (ExpvarPlaybackMetrics) NowPlayingLookupTimedOut()    { nowPlayingLookupTimeouts.Add(1) }
func (ExpvarPlaybackMetrics) QueueStateRateLimited()       { queueStateRateLimited.Add(1) }

func (ExpvarPlaybackMetrics) SweepIdle()             { erasureSweepIdle.Add(1) }
func (ExpvarPlaybackMetrics) QueueStateErased(n int) { queueStateErased.Add(int64(n)) }
func (ExpvarPlaybackMetrics) OwnerScanTimedOut()     { erasureOwnerScanTimeouts.Add(1) }
func (ExpvarPlaybackMetrics) RetryOwnerDropped()     { erasureRetryOwnersDropped.Add(1) }

func (ExpvarPlaybackMetrics) EnrichmentBreakerRejected() { enrichmentBreakerRejections.Add(1) }
func (ExpvarPlaybackMetrics) EnrichmentBreakerOpened()   { enrichmentBreakerOpen.Set(breakerDegraded) }
func (ExpvarPlaybackMetrics) EnrichmentBreakerClosed()   { enrichmentBreakerOpen.Set(breakerHealthy) }

type Snapshot struct {
	EnrichmentFailures          int64 `json:"now_playing_enrichment_failures_total"`
	CorruptStoredState          int64 `json:"corrupt_stored_state_total"`
	QueueStateOpTimeouts        int64 `json:"queue_state_op_timeouts_total"`
	QueueStateTransientFailures int64 `json:"queue_state_op_transient_failures_total"`
	QueueStateStaleWrites       int64 `json:"queue_state_stale_writes_total"`
	NowPlayingLookupTimeouts    int64 `json:"now_playing_lookup_timeouts_total"`
	QueueStateRateLimited       int64 `json:"queue_state_rate_limited_total"`
	ErasureSweepIdle            int64 `json:"erasure_sweep_idle_total"`
	QueueStateErased            int64 `json:"queue_state_erased_total"`
	ErasureOwnerScanTimeouts    int64 `json:"erasure_owner_scan_timeouts_total"`
	ErasureRetryOwnersDropped   int64 `json:"erasure_retry_owners_dropped_total"`

	EnrichmentBreakerRejections int64 `json:"now_playing_enrichment_breaker_rejections_total"`
	EnrichmentBreakerOpen       bool  `json:"now_playing_enrichment_breaker_open"`
}

func ReadSnapshot() Snapshot {
	return Snapshot{
		EnrichmentFailures:          enrichmentFailures.Value(),
		CorruptStoredState:          corruptStoredState.Value(),
		QueueStateOpTimeouts:        queueStateOpTimeouts.Value(),
		QueueStateTransientFailures: queueStateTransientFailures.Value(),
		QueueStateStaleWrites:       queueStateStaleWrites.Value(),
		NowPlayingLookupTimeouts:    nowPlayingLookupTimeouts.Value(),
		QueueStateRateLimited:       queueStateRateLimited.Value(),
		ErasureSweepIdle:            erasureSweepIdle.Value(),
		QueueStateErased:            queueStateErased.Value(),
		ErasureOwnerScanTimeouts:    erasureOwnerScanTimeouts.Value(),
		ErasureRetryOwnersDropped:   erasureRetryOwnersDropped.Value(),

		EnrichmentBreakerRejections: enrichmentBreakerRejections.Value(),
		EnrichmentBreakerOpen:       enrichmentBreakerOpen.Value() == breakerDegraded,
	}
}
