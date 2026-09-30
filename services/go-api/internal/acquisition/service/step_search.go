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

type outageReportingFinder interface {
	FindReportingOutage(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error, error)
}

type SearchStep struct {
	finder candidateFinder
}

func NewSearchStep(finder candidateFinder) *SearchStep {
	return &SearchStep{finder: finder}
}

func (s *SearchStep) Name() StepName { return stepNameSearch }

func (s *SearchStep) Execute(ctx context.Context, ac *AcquisitionContext, _ pipelineStart) (afterSearch, error) {
	candidates, outage, err := s.find(ctx, findRequestFor(ac))
	if err != nil {
		return afterSearch{}, withCancellation(ctx, err)
	}

	kept, skipped := filterCandidates(ctx, ac, candidates)
	if len(kept) == 0 && len(skipped) > 0 {
		slog.InfoContext(ctx, "acquisition.prior_rejections_exhausted",
			"track_id", ac.Track.ID, "skipped", len(skipped))
		kept = skipped
	}
	if len(kept) == 0 {
		return afterSearch{}, withCancellation(ctx, fmt.Errorf("no candidates found"))
	}

	ac.Candidates = kept
	ac.SearchUnavailable = outage
	return afterSearch{}, nil
}

func (s *SearchStep) find(ctx context.Context, req ports.FindRequest) ([]ports.AudioCandidate, error, error) {
	if reporter, ok := s.finder.(outageReportingFinder); ok {
		return reporter.FindReportingOutage(ctx, req)
	}
	candidates, err := s.finder.Find(ctx, req)
	return candidates, nil, err
}

func filterCandidates(ctx context.Context, ac *AcquisitionContext, candidates []ports.AudioCandidate) (kept, skipped []ports.AudioCandidate) {
	for _, c := range candidates {
		switch {
		case ac.Replace.excludes(c.URL):
			slog.InfoContext(ctx, "acquisition.candidate_excluded",
				"track_id", ac.Track.ID, "url", c.URL, "source", c.Source)
		case ac.priorRejected(c.URL):
			slog.InfoContext(ctx, "acquisition.candidate_skipped_prior_rejection",
				"track_id", ac.Track.ID, "key", sourceKey(c.URL), "source", c.Source)
			skipped = append(skipped, c)
		default:
			kept = append(kept, c)
		}
	}
	return kept, skipped
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
