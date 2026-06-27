package service

import (
	"context"
	"fmt"
)

type SelectStep struct{}

func NewSelectStep() *SelectStep { return &SelectStep{} }

func (s *SelectStep) Name() string { return "select" }

func (s *SelectStep) Execute(ctx context.Context, ac *AcquisitionContext) error {
	ranked := rankCandidates(ctx, ac.Track, ac.Candidates)
	if len(ranked) == 0 {
		return fmt.Errorf("no candidates passed matching gates")
	}
	ac.Ranked = ranked
	// Provisional best for telemetry; DownloadStep advances past it if a
	// downloaded file fails verification.
	best := ranked[0]
	ac.Selected = &best
	return nil
}

func (s *SelectStep) Rollback(_ context.Context, _ *AcquisitionContext) error {
	return nil
}
