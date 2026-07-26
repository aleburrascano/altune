package service

import (
	"context"
	"fmt"
	"log/slog"
)

type SelectStep struct{}

func NewSelectStep() *SelectStep { return &SelectStep{} }

func (s *SelectStep) Name() string { return "select" }

func (s *SelectStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	ranked := rankCandidates(ctx, ac.Track, ac.Candidates)
	if ac.SkipTopRanked && len(ranked) > 0 {
		slog.InfoContext(ctx, "acquisition.skipped_top_ranked_unknown_source",
			"url", ranked[0].URL, "track_id", ac.Track.ID)
		ranked = ranked[1:]
	}
	if len(ranked) == 0 {
		return fmt.Errorf("no candidates passed matching gates")
	}
	ac.Ranked = ranked
	best := ranked[0]
	ac.Selected = &best
	return nil
}

func (s *SelectStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}
