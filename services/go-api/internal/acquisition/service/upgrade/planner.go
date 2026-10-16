package upgrade

import (
	acqdomain "altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"
)

type Scheduler interface {
	ScheduleUpgrade(ctx context.Context, userId shared.UserId, trackId shared.TrackId) error
}

type Planner struct {
	store        ports.UpgradeStore
	scheduler    Scheduler
	maxQueued    int
	retryAfter   time.Duration
	checkUploads bool
	lowBitrate   int
}

func NewPlanner(store ports.UpgradeStore, scheduler Scheduler, maxQueued int, retryAfter time.Duration) *Planner {
	return &Planner{store: store, scheduler: scheduler, maxQueued: maxQueued, retryAfter: retryAfter}
}

func (p *Planner) CheckingUploads() *Planner {
	p.checkUploads = true
	return p
}

func (p *Planner) RescuingLowBitrate(floorKbps int) *Planner {
	p.lowBitrate = floorKbps
	return p
}

func (p *Planner) PlanNext(ctx context.Context) error {
	candidates, err := p.store.Candidates(ctx, ports.UpgradeSelection{
		Limit:        1,
		Generation:   grade.PipelineGeneration,
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

func (p *Planner) schedule(ctx context.Context, candidate ports.UpgradeCandidate) error {
	err := p.scheduler.ScheduleUpgrade(ctx, candidate.UserID, candidate.TrackID)
	if errors.Is(err, acqdomain.ErrAcquisitionPaused) {
		slog.InfoContext(ctx, "acquisition.upgrade_planning_paused")
		return nil
	}
	if errors.Is(err, acqdomain.ErrTrackJobInFlight) {
		slog.InfoContext(ctx, "acquisition.upgrade_skipped_job_in_flight", "track_id", candidate.TrackID.String())
		return nil
	}
	if err != nil {
		return fmt.Errorf("schedule upgrade of track %s: %w", candidate.TrackID, err)
	}
	slog.InfoContext(ctx, "acquisition.upgrade_scheduled",
		"track_id", candidate.TrackID.String(), "grade", candidate.Grade)
	return nil
}

func Interval(perHour int) time.Duration {
	return time.Hour / time.Duration(max(perHour, 1))
}
