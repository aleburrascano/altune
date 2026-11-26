package service

import (
	"altune/go-api/internal/download/internal/core/candidateranking"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/internal/core/rejectedcandidates"
	"context"
	"fmt"
)

type SelectStep struct{}

func NewSelectStep() *SelectStep { return &SelectStep{} }

func (s *SelectStep) Name() StepName { return stepNameSelect }

func (s *SelectStep) Execute(ctx context.Context, ac *AcquisitionContext, _ afterSearch) (AfterSelect, error) {
	ranked, rejected := rankAndCollect(ctx, ac.Track, ac.ExpectedLength(), ac.Candidates)
	ac.Rejections = append(ac.Rejections, rejected...)
	if len(ranked) == 0 && ac.SearchUnavailable != nil {
		return AfterSelect{}, fmt.Errorf("no candidates passed matching gates: %w", ac.SearchUnavailable)
	}
	if len(ranked) == 0 {
		return AfterSelect{}, fmt.Errorf("no candidates passed matching gates")
	}
	ac.Ranked = ranked
	best := ranked[0]
	ac.Selected = &best
	return AfterSelect{}, nil
}

func (s *SelectStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}

func rankAndCollect(ctx context.Context, track TrackRef, expectedLength float64, candidates []ports.AudioCandidate) ([]ports.AudioCandidate, []rejectedcandidates.Entry) {
	ranked, refused := candidateranking.Rank(ctx, candidateranking.Track{
		ID:              track.ID,
		Title:           track.Title,
		Artist:          track.Artist,
		ExpectedLength:  expectedLength,
		CatalogExplicit: track.CatalogExplicit,
	}, candidates)
	var rejected []rejectedcandidates.Entry
	for _, r := range refused {
		c := r.Candidate
		rejected = append(rejected, rejectedcandidates.Entry{URL: c.URL, Title: c.Title, Source: c.Source, Stage: rejectedcandidates.Stage(r.Stage), Reason: r.Reason})
	}
	return ranked, rejected
}
