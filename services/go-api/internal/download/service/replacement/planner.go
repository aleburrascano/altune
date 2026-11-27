package replacement

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	acqdomain "altune/go-api/internal/download/internal/core/domain"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Scheduler interface {
	ScheduleUpgrade(ctx context.Context, userId ids.UserId, trackId ids.TrackId) error
}

type ReplacementPlanner struct {
	store        ports.UpgradeStore
	scheduler    Scheduler
	maxQueued    int
	retryAfter   time.Duration
	checkUploads bool
	lowBitrate   int
}

func NewReplacementPlanner(store ports.UpgradeStore, scheduler Scheduler, maxQueued int, retryAfter time.Duration) *ReplacementPlanner {
	return &ReplacementPlanner{store: store, scheduler: scheduler, maxQueued: maxQueued, retryAfter: retryAfter}
}

func (p *ReplacementPlanner) CheckingUploads() *ReplacementPlanner {
	p.checkUploads = true
	return p
}

func (p *ReplacementPlanner) RescuingLowBitrate(floorKbps int) *ReplacementPlanner {
	p.lowBitrate = floorKbps
	return p
}

func (p *ReplacementPlanner) PlanNext(ctx context.Context) error {
	candidates, err := p.store.Candidates(ctx, ports.UpgradeSelection{
		Limit:        1,
		Generation:   audioquality.PipelineGeneration,
		RetryAfter:   p.retryAfter,
		MaxQueued:    p.maxQueued,
		CheckInPlace: p.checkUploads,

		LowBitrateFloorKbps: p.lowBitrate,
	})
	if err != nil {
		return fmt.Errorf("pick tracks to upgrade: %w", err)
	}
	for _, candidate := range candidates {
		if err := p.schedule(ctx, candidate); err != nil {
			return err
		}
	}
	return nil
}

func (p *ReplacementPlanner) schedule(ctx context.Context, candidate ports.UpgradeCandidate) error {
	err := p.scheduler.ScheduleUpgrade(ctx, candidate.UserID, candidate.TrackID)
	if errors.Is(err, acqdomain.ErrAcquisitionPaused) {
		slog.InfoContext(ctx, "acquisition.upgrade_planning_paused")
		return nil
	}
	if errors.Is(err, acqdomain.ErrTrackJobInFlight) {
		slog.InfoContext(ctx, "acquisition.upgrade_skipped_job_in_flight", logging.KeyTrackID, candidate.TrackID.String())
		return nil
	}
	if err != nil {
		return fmt.Errorf("schedule upgrade of track %s: %w", candidate.TrackID, err)
	}
	slog.InfoContext(ctx, "acquisition.upgrade_scheduled",
		logging.KeyTrackID, candidate.TrackID.String(), "grade", candidate.Grade)
	return nil
}

func Interval(perHour int) time.Duration {
	return time.Hour / time.Duration(max(perHour, 1))
}
