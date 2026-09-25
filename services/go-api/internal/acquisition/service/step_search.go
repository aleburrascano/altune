package service

import (
	"altune/go-api/internal/acquisition/ports"
	"context"
	"fmt"
	"log/slog"
)

type candidateFinder interface {
	Find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error)
}

type SearchStep struct {
	finder candidateFinder
}

func NewSearchStep(finder candidateFinder) *SearchStep {
	return &SearchStep{finder: finder}
}

func (s *SearchStep) Name() string { return stepNameSearch }

func (s *SearchStep) Execute(ctx context.Context, ac *AcquisitionContext, _ pipelineStart) (afterSearch, error) {
	candidates, err := s.finder.Find(ctx, findRequestFor(ac))
	if err != nil {
		return afterSearch{}, withCancellation(ctx, err)
	}

	kept := make([]ports.AudioCandidate, 0, len(candidates))
	for _, c := range candidates {
		if ac.Replace.excludes(c.URL) {
			slog.InfoContext(ctx, "acquisition.candidate_excluded",
				"track_id", ac.Track.ID, "url", c.URL, "source", c.Source)
			continue
		}
		kept = append(kept, c)
	}
	if len(kept) == 0 {
		// Sources that swallow their own cancellation surface as an empty result.
		return afterSearch{}, withCancellation(ctx, fmt.Errorf("no candidates found"))
	}

	ac.Candidates = kept
	return afterSearch{}, nil
}

func (s *SearchStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}

func findRequestFor(ac *AcquisitionContext) ports.FindRequest {
	return ports.FindRequest{
		Title:    ac.Track.Title,
		Artist:   ac.Track.Artist,
		Album:    ac.Track.Album,
		ISRC:     ac.Track.ISRC,
		Duration: ac.Track.Duration,
		Identity: ac.Identity,
	}
}
