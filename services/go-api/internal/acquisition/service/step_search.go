package service

import (
	"context"
	"fmt"

	"altune/go-api/internal/acquisition/ports"
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

func (s *SearchStep) Name() string { return "search" }

func (s *SearchStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	candidates, err := s.finder.Find(ctx, findRequestFor(ac))
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return fmt.Errorf("no candidates found")
	}

	ac.Candidates = candidates
	return nil
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
