// Package metrics provides an expvar-backed implementation of the playback
// metrics port. expvar is stdlib, so it adds no dependency: the counters are
// process-global published integers that can be scraped from an expvar
// endpoint (wiring such an endpoint is intentionally out of scope here).
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
)

// Declared at package scope because expvar.NewInt panics on a duplicate name;
// registering once keeps the adapter safe to construct any number of times.
var (
	enrichmentFailures       = expvar.NewInt(EnrichmentFailuresVar)
	corruptStoredState       = expvar.NewInt(CorruptStoredStateVar)
	queueStateOpTimeouts     = expvar.NewInt(QueueStateOpTimeoutsVar)
	nowPlayingLookupTimeouts = expvar.NewInt(NowPlayingLookupTimeoutsVar)
)

// ExpvarPlaybackMetrics implements ports.PlaybackMetrics by incrementing
// process-global expvar counters.
type ExpvarPlaybackMetrics struct{}

var _ ports.PlaybackMetrics = ExpvarPlaybackMetrics{}

// NewExpvarPlaybackMetrics returns an ExpvarPlaybackMetrics.
func NewExpvarPlaybackMetrics() ExpvarPlaybackMetrics { return ExpvarPlaybackMetrics{} }

func (ExpvarPlaybackMetrics) EnrichmentFailed()         { enrichmentFailures.Add(1) }
func (ExpvarPlaybackMetrics) CorruptStoredState()       { corruptStoredState.Add(1) }
func (ExpvarPlaybackMetrics) QueueStateOpTimedOut()     { queueStateOpTimeouts.Add(1) }
func (ExpvarPlaybackMetrics) NowPlayingLookupTimedOut() { nowPlayingLookupTimeouts.Add(1) }
