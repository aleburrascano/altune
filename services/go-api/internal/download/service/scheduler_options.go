package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"time"
)

func WithSchedulerPaused(paused bool) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.paused.Store(paused) }
}

func WithJobQueue(q ports.DownloadQueue) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if q == nil {
			s.setupErr = errors.Join(s.setupErr, missingPort("WithJobQueue", "queue", true))
			return
		}
		s.queue = q
	}
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

func WithVerificationStatus(v ports.Verification) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		s.verification = v
		if !v.ToolsPresent() {
			slog.WarnContext(context.Background(), "acquisition.verification_degraded",
				"ffprobe", v.Ffprobe, "ffmpeg", v.Ffmpeg, "fpcalc", v.Fpcalc, "yt_dlp", v.YtDlp,
				"fingerprint_lookup", fingerprintLookupUnverified)
			return
		}
		slog.InfoContext(context.Background(), "acquisition.verification_armed",
			"fpcalc", v.Fpcalc, "fingerprint_lookup", fingerprintLookupUnverified)
	}
}

type upgradeRunner interface {
	ExecuteUpgrade(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error
}

func WithUpgradeLane(runner upgradeRunner, workers int) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		s.upgrader = runner
		s.upgradeWorkers = workers
	}
}

type VerifySkipCounter struct{ n atomic.Uint64 }

func (c *VerifySkipCounter) RecordVerifySkip(string) { c.n.Add(1) }

func (c *VerifySkipCounter) Count() uint64 { return c.n.Load() }
