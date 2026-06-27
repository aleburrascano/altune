package service

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

type Step interface {
	Name() string
	Execute(ctx context.Context, ac *AcquisitionContext) error
	Rollback(ctx context.Context, ac *AcquisitionContext) error
}

// StepError identifies which pipeline step failed. It carries the step name as a
// field so callers (failureReason) map outcomes on the structured Step, not by
// parsing an error-message prefix. Its Error() preserves the historical
// "step <name>: <err>" format so logs and any string matchers stay stable.
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
	Track      TrackRef
	Candidates []Candidate
	// Ranked is the best-first candidate list produced by SelectStep; DownloadStep
	// walks it, downloading and verifying each until one passes.
	Ranked   []Candidate
	Selected *Candidate
	TempPath string
	AudioRef string
}

type TrackRef struct {
	ID           string
	UserID       string
	Title        string
	Artist       string
	Album        string
	Duration     float64
	ISRC         string
	Year         int
	TrackNumber  int
	AlbumArtist  string
	Genre        string
}

type Candidate struct {
	Title        string
	Artist       string
	Duration     float64
	URL          string
	Channel      string
	Categories   []string
	ViewCount    int64
	FollowerCount int64
}
