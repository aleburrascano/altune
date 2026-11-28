package service

import (
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/logging"
	"context"
	"fmt"
	"log/slog"
)

type candidateFinder interface {
	FindReportingOutage(ctx context.Context, req ports.FindRequest) (FindResult, error)
}

type SearchStep struct {
	finder candidateFinder
}

func NewSearchStep(finder candidateFinder) *SearchStep {
	return &SearchStep{finder: finder}
}

func (s *SearchStep) Name() StepName { return stepNameSearch }

func (s *SearchStep) Execute(ctx context.Context, ac *AcquisitionContext, _ PipelineStart) (afterSearch, error) {
	res, err := s.find(ctx, findRequestFor(ac))
	if err != nil {
		return afterSearch{}, withCancellation(ctx, err)
	}
	outage := res.Outage

	ac.Pass = passAfterSearch(ac.Pass, res)
	kept, skipped := filterCandidates(ctx, ac, res.Candidates)
	if len(kept) == 0 && len(skipped) > 0 {
		slog.InfoContext(ctx, "acquisition.prior_rejections_exhausted",
			logging.KeyTrackID, ac.Track.ID, "skipped", len(skipped))
		kept = skipped
	}
	if len(kept) == 0 && outage != nil {
		return afterSearch{}, withCancellation(ctx, fmt.Errorf("no candidates found: %w", outage))
	}
	if len(kept) == 0 {
		return afterSearch{}, withCancellation(ctx, fmt.Errorf("no candidates found"))
	}

	ac.Candidates = kept
	ac.SearchUnavailable = outage
	return afterSearch{}, nil
}

func (s *SearchStep) find(ctx context.Context, req ports.FindRequest) (FindResult, error) {
	found, err := s.finder.FindReportingOutage(ctx, req)
	if err != nil {
		return found, fmt.Errorf("find candidates: %w", err)
	}
	return found, nil
}

func filterCandidates(ctx context.Context, ac *AcquisitionContext, candidates []ports.AudioCandidate) (kept, skipped []ports.AudioCandidate) {
	for _, c := range candidates {
		switch {
		case ac.Replace.excludesCandidate(c):
			slog.InfoContext(ctx, "acquisition.candidate_excluded",
				logging.KeyTrackID, ac.Track.ID, "url", c.URL, "source", c.Source)
		case ac.priorRejected(c.URL):
			slog.InfoContext(ctx, "acquisition.candidate_skipped_prior_rejection",
				logging.KeyTrackID, ac.Track.ID, "key", sourceKey(c.URL), "source", c.Source)
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
		ISRC:     searchHintISRC(ac),
		Duration: ac.ExpectedLength(),
		Identity: ac.Identity,

		CatalogSources:  ac.Track.CatalogSources,
		CatalogExplicit: ac.Track.CatalogExplicit,

		KnownSourceURL: ac.Track.AudioSourceURL,

		Tier: tierOf(ac.Pass),
	}
}

func searchHintISRC(ac *AcquisitionContext) string {
	if ac.Track.ISRC != "" {
		return ac.Track.ISRC
	}
	return ac.Identity.ISRC
}

type SearchPass int

const (
	PassAllTiers SearchPass = iota
	PassFallbackSkipped
	PassFallbackOnly
)

func tierOf(pass SearchPass) ports.SourceTier {
	if pass == PassFallbackOnly {
		return ports.TierFallbackOnly
	}
	return ports.TierFirstThenFallback
}

func passAfterSearch(pass SearchPass, res FindResult) SearchPass {
	switch {
	case pass == PassFallbackOnly:
		return PassFallbackOnly
	case res.FallbackSkipped:
		return PassFallbackSkipped
	default:
		return PassAllTiers
	}
}
