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

func RunPipeline(ctx context.Context, steps []Step, ac *AcquisitionContext) error {
	var completed []Step

	for _, step := range steps {
		if err := ctx.Err(); err != nil {
			rollback(ctx, completed, ac)
			return fmt.Errorf("pipeline cancelled: %w", err)
		}

		slog.InfoContext(ctx, "pipeline step starting", "step", step.Name())

		if err := step.Execute(ctx, ac); err != nil {
			slog.ErrorContext(ctx, "pipeline step failed",
				"step", step.Name(), "error", err)
			rollback(ctx, completed, ac)
			return fmt.Errorf("step %s: %w", step.Name(), err)
		}

		completed = append(completed, step)
	}

	return nil
}

func rollback(_ context.Context, completed []Step, ac *AcquisitionContext) {
	rbCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
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
	Track     TrackRef
	Candidates []Candidate
	Selected  *Candidate
	TempPath  string
	AudioRef  string
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
	Score        float64
}
