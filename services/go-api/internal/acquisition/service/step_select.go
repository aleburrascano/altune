package service

import (
	"altune/go-api/internal/acquisition/service/judge"
	"altune/go-api/internal/acquisition/service/judgeshadow"
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
	judgeshadow.Log(ctx, shadowTarget(ac.Track), ac.Track.ID, best, ac.Candidates)
	return afterSelect{}, nil
}

func shadowTarget(t TrackRef) judge.Target {
	return judge.Target{Title: t.Title, Artist: t.Artist, Album: t.Album, Length: t.CatalogDuration, Explicit: t.CatalogExplicit}
}

func (s *SelectStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}
