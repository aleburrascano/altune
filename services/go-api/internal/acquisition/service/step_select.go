package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"log/slog"
)

type SelectStep struct{}

func NewSelectStep() *SelectStep { return &SelectStep{} }

func (s *SelectStep) Name() StepName { return stepNameSelect }

func (s *SelectStep) Execute(ctx context.Context, ac *AcquisitionContext, _ afterSearch) (afterSelect, error) {
	ranked, rejected := rankAndCollect(ctx, ac.Track, ac.Candidates)
	ac.Rejections = append(ac.Rejections, rejected...)
	if ac.Replace.SkipTopRanked {
		ranked = skipTopUpload(ctx, ac.Track.ID, ranked)
	}
	if len(ranked) == 0 {
		if ac.SearchUnavailable != nil {
			return afterSelect{}, fmt.Errorf("no candidates passed matching gates: %w", ac.SearchUnavailable)
		}
		return afterSelect{}, fmt.Errorf("no candidates passed matching gates")
	}
	ac.Ranked = ranked
	best := ranked[0]
	ac.Selected = &best
	return afterSelect{}, nil
}

func skipTopUpload(ctx context.Context, trackID string, ranked []ports.AudioCandidate) []ports.AudioCandidate {
	for i, c := range ranked {
		if c.Resolved {
			continue
		}
		slog.InfoContext(ctx, "acquisition.skipped_top_ranked_unknown_source", "url", c.URL, "track_id", trackID)
		return append(ranked[:i:i], ranked[i+1:]...)
	}
	return ranked
}

func (s *SelectStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}
