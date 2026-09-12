// Package metrics provides an expvar-backed implementation of the catalog
// audio-store metrics port. expvar is stdlib, so it adds no dependency: the
// counters are process-global published integers that can be scraped from an
// expvar endpoint (wiring such an endpoint is intentionally out of scope here).
package metrics

import (
	"expvar"

	"altune/go-api/internal/catalog/ports"
)

// Published expvar variable names for the audio-store degradation counters.
const (
	PresignFailuresVar  = "catalog_audio_presign_failures_total"
	OrphanedDeletesVar  = "catalog_audio_orphaned_deletes_total"
	StreamRecoveriesVar = "catalog_audio_stream_recoveries_total"
)

// Declared at package scope because expvar.NewInt panics on a duplicate name;
// registering once keeps the adapter safe to construct any number of times.
var (
	presignFailures  = expvar.NewInt(PresignFailuresVar)
	orphanedDeletes  = expvar.NewInt(OrphanedDeletesVar)
	streamRecoveries = expvar.NewInt(StreamRecoveriesVar)
)

// ExpvarAudioStoreMetrics implements ports.AudioStoreMetrics by incrementing
// process-global expvar counters.
type ExpvarAudioStoreMetrics struct{}

var _ ports.AudioStoreMetrics = ExpvarAudioStoreMetrics{}

// NewExpvarAudioStoreMetrics returns an ExpvarAudioStoreMetrics.
func NewExpvarAudioStoreMetrics() ExpvarAudioStoreMetrics { return ExpvarAudioStoreMetrics{} }

func (ExpvarAudioStoreMetrics) PresignFailed()           { presignFailures.Add(1) }
func (ExpvarAudioStoreMetrics) OrphanedDelete()          { orphanedDeletes.Add(1) }
func (ExpvarAudioStoreMetrics) StreamRecoveryTriggered() { streamRecoveries.Add(1) }
