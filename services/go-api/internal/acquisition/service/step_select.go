package service

import (
	"context"
	"fmt"
)

type SelectStep struct{}

func NewSelectStep() *SelectStep { return &SelectStep{} }

func (s *SelectStep) Name() string { return "select" }

func (s *SelectStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	selected := selectBestCandidate(ctx, ac.Track, ac.Candidates)
	if selected == nil {
		return fmt.Errorf("no candidates passed matching gates")
	}
	ac.Selected = selected
	return nil
}

func (s *SelectStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}
