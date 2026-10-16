package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"log/slog"
	"sync/atomic"
	"time"
)

func WithSchedulerEvents(pub events.Publisher) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if pub != nil {
			s.events = pub
		}
	}
}

func WithSchedulerPaused(paused bool) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.paused.Store(paused) }
}

func WithJobQueue(q ports.JobQueue) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) {
		if q != nil {
			s.queue = q
		}
	}
}

func WithJobNotifier(n ports.JobNotifier) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.notifier = n }
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

func WithOutcomeRecorder(r ports.OutcomeRecorder) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.outcomes = r }
}

const fingerprintLookupUnverified = "unverified until the first successful lookup"

func WithVerificationStatus(v ports.AcquisitionVerification) func(*BackgroundAcquisitionScheduler) {
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

func WithVerifySkipCount(count func() uint64) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.skipCount = count }
}

func WithFingerprintVerified(verified func() bool) func(*BackgroundAcquisitionScheduler) {
	return func(s *BackgroundAcquisitionScheduler) { s.fingerprintVerified = verified }
}

type upgradeRunner interface {
	ExecuteUpgrade(ctx context.Context, userId shared.UserId, trackId shared.TrackId) error
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
