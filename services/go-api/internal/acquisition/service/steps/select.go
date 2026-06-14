package steps

import (
	"context"
	"fmt"

	"altune/go-api/internal/acquisition/service"
)

type SelectStep struct{}

func NewSelectStep() *SelectStep { return &SelectStep{} }

func (s *SelectStep) Name() string { return "select" }

func (s *SelectStep) Execute(_ context.Context, ac *service.AcquisitionContext) error {
	selected := service.SelectBestCandidate(ac.Track, ac.Candidates)
	if selected == nil {
		return fmt.Errorf("no candidates passed matching gates")
	}
	ac.Selected = selected
	return nil
}

func (s *SelectStep) Rollback(_ context.Context, _ *service.AcquisitionContext) error {
	return nil
}
