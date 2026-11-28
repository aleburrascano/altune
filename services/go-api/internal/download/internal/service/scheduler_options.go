package service

import (
	"altune/go-api/internal/download/internal/ports"
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

func WithSchedulerPaused(paused bool) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.gate.paused.Store(paused) }
}

func WithDrainBudget(d time.Duration) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if d > 0 {
			s.drainBudget = d
		}
	}
}

func WithLeaseDuration(d time.Duration) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if d > 0 {
			s.leaseDuration = d
		}
	}
}

func WithHeartbeatInterval(d time.Duration) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if d > 0 {
			s.heartbeatInterval = d
		}
	}
}

func WithPollInterval(d time.Duration) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if d > 0 {
			s.pollInterval = d
		}
	}
}

const fingerprintLookupUnverified = "unverified until the first successful lookup"

type upgradeRunner interface {
	ExecuteUpgrade(ctx context.Context, job ports.AcquisitionJob) error
}

type VerifySkipCounter struct{ n atomic.Uint64 }

func (c *VerifySkipCounter) RecordVerifySkip(string) { c.n.Add(1) }

func (c *VerifySkipCounter) Count() uint64 { return c.n.Load() }

func logVerification(ctx context.Context, v ports.Verification) {
	if !v.ToolsPresent() {
		slog.WarnContext(ctx, "acquisition.verification_degraded",
			"ffprobe", v.Ffprobe, "ffmpeg", v.Ffmpeg, "fpcalc", v.Fpcalc, "yt_dlp", v.YtDlp,
			"fingerprint_lookup", fingerprintLookupUnverified)
		return
	}
	slog.InfoContext(ctx, "acquisition.verification_armed",
		"fpcalc", v.Fpcalc, "fingerprint_lookup", fingerprintLookupUnverified)
}
