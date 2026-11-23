package service

import (
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/internal/core/ranking"
	"altune/go-api/internal/acquisition/internal/core/rejectionlog"
	"context"
	"fmt"
)

type SelectStep struct{}

func NewSelectStep() *SelectStep { return &SelectStep{} }

func (s *SelectStep) Name() StepName { return stepNameSelect }

func (s *SelectStep) Execute(ctx context.Context, ac *AcquisitionContext, _ afterSearch) (afterSelect, error) {
	ranked, rejected := rankAndCollect(ctx, ac.Track, ac.ExpectedLength(), ac.Candidates)
	ac.Rejections = append(ac.Rejections, rejected...)
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

func (s *SelectStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}

func rankAndCollect(ctx context.Context, track TrackRef, expectedLength float64, candidates []ports.AudioCandidate) ([]ports.AudioCandidate, []rejectionlog.Entry) {
	ranked, refused := ranking.Rank(ctx, ranking.Track{
		ID:              track.ID,
		Title:           track.Title,
		Artist:          track.Artist,
		ExpectedLength:  expectedLength,
		CatalogExplicit: track.CatalogExplicit,
	}, candidates)
	var rejected []rejectionlog.Entry
	for _, r := range refused {
		c := r.Candidate
		rejected = append(rejected, rejectionlog.Entry{URL: c.URL, Title: c.Title, Source: c.Source, Stage: rejectionlog.Stage(r.Stage), Reason: r.Reason})
	}
	return ranked, rejected
}
