package telemetry

import (
	"altune/go-api/internal/playback/internal/ports"
	"altune/go-api/internal/shared/telemetry"
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

const (
	breakerDegraded = 1
	breakerHealthy  = 0
)

type RegistryPlaybackMetrics struct {
	enrichmentFailures          *telemetry.Counter
	corruptStoredState          *telemetry.Counter
	queueStateOpTimeouts        *telemetry.Counter
	queueStateTransientFailures *telemetry.Counter
	queueStateStaleWrites       *telemetry.Counter
	nowPlayingLookupTimeouts    *telemetry.Counter
	queueStateRateLimited       *telemetry.Counter
	erasureSweepIdle            *telemetry.Counter
	queueStateErased            *telemetry.Counter
	erasureOwnerScanTimeouts    *telemetry.Counter
	erasureRetryOwnersDropped   *telemetry.Counter

	enrichmentBreakerRejections *telemetry.Counter
	enrichmentBreakerOpen       *telemetry.Gauge
}

var (
	_ ports.EnrichmentMetrics = RegistryPlaybackMetrics{}
	_ ports.QueueStateMetrics = RegistryPlaybackMetrics{}
	_ ports.RateLimitMetrics  = RegistryPlaybackMetrics{}

	_ ports.ErasureSweepMetrics = RegistryPlaybackMetrics{}
)

func NewPlaybackMetrics(reg *telemetry.Registry) RegistryPlaybackMetrics {
	return RegistryPlaybackMetrics{
		enrichmentFailures:          reg.Counter(EnrichmentFailuresVar),
		corruptStoredState:          reg.Counter(CorruptStoredStateVar),
		queueStateOpTimeouts:        reg.Counter(QueueStateOpTimeoutsVar),
		queueStateTransientFailures: reg.Counter(QueueStateTransientFailuresVar),
		queueStateStaleWrites:       reg.Counter(QueueStateStaleWritesVar),
		nowPlayingLookupTimeouts:    reg.Counter(NowPlayingLookupTimeoutsVar),
		queueStateRateLimited:       reg.Counter(QueueStateRateLimitedVar),
		erasureSweepIdle:            reg.Counter(ErasureSweepIdleVar),
		queueStateErased:            reg.Counter(QueueStateErasedVar),
		erasureOwnerScanTimeouts:    reg.Counter(ErasureOwnerScanTimeoutsVar),
		erasureRetryOwnersDropped:   reg.Counter(ErasureRetryOwnersDroppedVar),

		enrichmentBreakerRejections: reg.Counter(EnrichmentBreakerRejectionsVar),
		enrichmentBreakerOpen:       reg.Gauge(EnrichmentBreakerOpenVar),
	}
}

func (m RegistryPlaybackMetrics) EnrichmentFailed()            { m.enrichmentFailures.Add(1) }
func (m RegistryPlaybackMetrics) CorruptStoredState()          { m.corruptStoredState.Add(1) }
func (m RegistryPlaybackMetrics) QueueStateOpTimedOut()        { m.queueStateOpTimeouts.Add(1) }
func (m RegistryPlaybackMetrics) QueueStateOpFailedTransient() { m.queueStateTransientFailures.Add(1) }
func (m RegistryPlaybackMetrics) QueueStateStaleWrite()        { m.queueStateStaleWrites.Add(1) }
func (m RegistryPlaybackMetrics) NowPlayingLookupTimedOut()    { m.nowPlayingLookupTimeouts.Add(1) }
func (m RegistryPlaybackMetrics) QueueStateRateLimited()       { m.queueStateRateLimited.Add(1) }

func (m RegistryPlaybackMetrics) SweepIdle()             { m.erasureSweepIdle.Add(1) }
func (m RegistryPlaybackMetrics) QueueStateErased(n int) { m.queueStateErased.Add(int64(n)) }
func (m RegistryPlaybackMetrics) OwnerScanTimedOut()     { m.erasureOwnerScanTimeouts.Add(1) }
func (m RegistryPlaybackMetrics) RetryOwnerDropped()     { m.erasureRetryOwnersDropped.Add(1) }

func (m RegistryPlaybackMetrics) EnrichmentBreakerRejected() { m.enrichmentBreakerRejections.Add(1) }

func (m RegistryPlaybackMetrics) EnrichmentBreakerOpened() {
	m.enrichmentBreakerOpen.Set(breakerDegraded)
}

func (m RegistryPlaybackMetrics) EnrichmentBreakerClosed() {
	m.enrichmentBreakerOpen.Set(breakerHealthy)
}

func ReadSnapshot(reg *telemetry.Registry) Snapshot {
	return Snapshot{
		EnrichmentFailures:          reg.Counter(EnrichmentFailuresVar).Value(),
		CorruptStoredState:          reg.Counter(CorruptStoredStateVar).Value(),
		QueueStateOpTimeouts:        reg.Counter(QueueStateOpTimeoutsVar).Value(),
		QueueStateTransientFailures: reg.Counter(QueueStateTransientFailuresVar).Value(),
		QueueStateStaleWrites:       reg.Counter(QueueStateStaleWritesVar).Value(),
		NowPlayingLookupTimeouts:    reg.Counter(NowPlayingLookupTimeoutsVar).Value(),
		QueueStateRateLimited:       reg.Counter(QueueStateRateLimitedVar).Value(),
		ErasureSweepIdle:            reg.Counter(ErasureSweepIdleVar).Value(),
		QueueStateErased:            reg.Counter(QueueStateErasedVar).Value(),
		ErasureOwnerScanTimeouts:    reg.Counter(ErasureOwnerScanTimeoutsVar).Value(),
		ErasureRetryOwnersDropped:   reg.Counter(ErasureRetryOwnersDroppedVar).Value(),

		EnrichmentBreakerRejections: reg.Counter(EnrichmentBreakerRejectionsVar).Value(),
		EnrichmentBreakerOpen:       reg.Gauge(EnrichmentBreakerOpenVar).Value() == breakerDegraded,
	}
}

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
