package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
)

type Step interface {
	Name() string
	Execute(ctx context.Context, ac *AcquisitionContext) error
	Rollback(ctx context.Context, ac *AcquisitionContext) error
}

type StepError struct {
	Step string
	Err  error
}

func (e *StepError) Error() string { return fmt.Sprintf("step %s: %v", e.Step, e.Err) }
func (e *StepError) Unwrap() error { return e.Err }

func RunPipeline(ctx context.Context, steps []Step, ac *AcquisitionContext) error {
	var completed []Step
	reporter := jobReporterFrom(ctx)

	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			rollback(ctx, completed, ac)
			return fmt.Errorf("pipeline cancelled: %w", err)
		}

		reporter.stage(step.Name())
		if ac.Selected != nil {
			reporter.source(ac.Selected.URL)
		}
		slog.InfoContext(ctx, "pipeline step starting", "step", step.Name(), "track_id", ac.Track.ID)

		if err := step.Execute(ctx, ac); err != nil {
			slog.ErrorContext(ctx, "pipeline step failed",
				"step", step.Name(), "track_id", ac.Track.ID, "error", err)
			rollback(ctx, completed, ac)
			return &StepError{Step: step.Name(), Err: err}
		}

		completed = append(completed, step)
	}

	return nil
}

func rollback(ctx context.Context, completed []Step, ac *AcquisitionContext) {
	rbCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	for i := len(completed) - 1; i >= 0; i-- {
		step := completed[i]
		slog.InfoContext(rbCtx, "rolling back step", "step", step.Name())
		if err := step.Rollback(rbCtx, ac); err != nil {
			slog.ErrorContext(rbCtx, "rollback failed", "step", step.Name(), "error", err)
		}
	}
}

type AcquisitionContext struct {
	Track            TrackRef
	Identity         ports.RecordingIdentity
	Candidates       []ports.AudioCandidate
	Ranked           []ports.AudioCandidate
	Selected         *ports.AudioCandidate
	TempPath         string
	AudioRef         string
	ProbedDuration   float64
	DurationVerified bool
	IdentityVerified bool

	ExcludeKeys   []string
	PreservedRef  string
	SkipTopRanked bool
}

func (ac *AcquisitionContext) excludes(url string) bool {
	key := sourceKey(url)
	for _, excluded := range ac.ExcludeKeys {
		if excluded == key {
			return true
		}
	}
	return false
}

func (ac *AcquisitionContext) Provenance() domain.AcquisitionProvenance {
	switch {
	case ac.IdentityVerified:
		return domain.ProvenanceVerified
	case ac.DurationVerified && ac.lengthCorroborated():
		return domain.ProvenanceCorroborated
	default:
		return domain.ProvenanceBestEffort
	}
}

func (ac *AcquisitionContext) MeasuredDuration() float64 {
	if ac.ProbedDuration > 0 {
		return ac.ProbedDuration
	}
	if ac.Selected != nil {
		return ac.Selected.Duration
	}
	return 0
}

func (ac *AcquisitionContext) lengthCorroborated() bool {
	saved, resolved := ac.Track.Duration, ac.Identity.Duration
	if resolved <= 0 {
		return false
	}
	if saved <= 0 {
		return true
	}
	return durationWithinAuthoritativeTolerance(saved, resolved)
}

func (ac *AcquisitionContext) durationAcceptable(actual float64) bool {
	saved, resolved := ac.Track.Duration, ac.Identity.Duration
	if ac.lengthCorroborated() {
		if saved <= 0 {
			return durationWithinAuthoritativeTolerance(resolved, actual)
		}
		return durationWithinAuthoritativeTolerance(saved, actual)
	}
	if resolved > 0 {
		return durationWithinTolerance(saved, actual) || durationWithinTolerance(resolved, actual)
	}
	return durationWithinTolerance(saved, actual)
}

type TrackRef struct {
	ID          string
	UserID      string
	Title       string
	Artist      string
	Album       string
	Duration    float64
	ISRC        string
	Year        int
	TrackNumber int
	AlbumArtist string
	Genre       string
}
