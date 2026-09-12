package service

import (
	"context"
	"fmt"
	"log/slog"
	"runtime/debug"
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

func RunPipeline(ctx context.Context, steps []Step, ac *AcquisitionContext) (err error) {
	var completed []Step
	var current Step
	reporter := jobReporterFrom(ctx)

	// A panic in any step must not skip rollback or propagate past this use
	// case: scheduler.go's recover is a last resort that bypasses acquire.go's
	// "mark track Failed" path, stranding the track at Pending forever. Mirror
	// registry.go's per-goroutine recover here — convert the panic into a
	// *StepError and roll back the completed steps so the normal failure path
	// runs.
	defer func() {
		if rec := recover(); rec != nil {
			step := "pipeline"
			if current != nil {
				step = current.Name()
			}
			slog.ErrorContext(ctx, "pipeline step panicked",
				"step", step, "track_id", ac.Track.ID, "panic", rec, "stack", string(debug.Stack()))
			rollback(ctx, completed, ac)
			err = &StepError{Step: step, Err: fmt.Errorf("panic: %v", rec)}
		}
	}()

	for _, step := range steps {
		if ctxErr := ctx.Err(); ctxErr != nil {
			rollback(ctx, completed, ac)
			return fmt.Errorf("pipeline cancelled: %w", ctxErr)
		}

		current = step
		reporter.stage(step.Name())
		if ac.Selected != nil {
			reporter.source(ac.Selected.URL)
		}
		slog.InfoContext(ctx, "pipeline step starting", "step", step.Name(), "track_id", ac.Track.ID)

		if execErr := step.Execute(ctx, ac); execErr != nil {
			slog.ErrorContext(ctx, "pipeline step failed",
				"step", step.Name(), "track_id", ac.Track.ID, "error", execErr)
			rollback(ctx, completed, ac)
			return &StepError{Step: step.Name(), Err: execErr}
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

	Replace ReplaceState
}

type ReplaceState struct {
	ExcludeKeys   []string
	PreservedRef  string
	SkipTopRanked bool
}

func (r ReplaceState) excludes(url string) bool {
	key := sourceKey(url)
	for _, excluded := range r.ExcludeKeys {
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
