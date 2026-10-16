package service

import (
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
