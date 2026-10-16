package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/shared"
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type UpdateTrackStep struct {
	ledger  ports.TrackLedger
	userId  shared.UserId
	trackId shared.TrackId
}

func NewUpdateTrackStep(ledger ports.TrackLedger, userId shared.UserId, trackId shared.TrackId) *UpdateTrackStep {
	return &UpdateTrackStep{
		ledger:  ledger,
		userId:  userId,
		trackId: trackId,
	}
}

func (s *UpdateTrackStep) Name() StepName { return stepNameUpdateTrack }

func (s *UpdateTrackStep) Execute(ctx context.Context, ac *AcquisitionContext, _ afterStore) (afterUpdate, error) {
	evidence := ac.collectEvidence()
	if err := ac.Replace.Gate.Admit(evidence.Grade); err != nil {
		return afterUpdate{}, err
	}
	isrc, _ := ac.provenISRC()
	completion := ports.Completion{
		AudioRef:           ac.AudioRef,
		ReplaceAudio:       ac.Replace.PreservedRef != "",
		Duration:           ac.MeasuredDuration(),
		ISRC:               isrc,
		Provenance:         ac.Provenance(),
		RejectedSourceKeys: ac.Replace.ExcludeKeys,
	}
	if ac.Selected != nil {
		raw, err := json.Marshal(evidence)
		if err != nil {
			return afterUpdate{}, fmt.Errorf("marshal acquisition evidence: %w", err)
		}
		completion.SourceURL = ac.Selected.URL
		completion.Confidence = ac.Confidence
		completion.Evidence = raw
	}
	if err := verifyCommitAllowed(ctx); err != nil {
		return afterUpdate{}, err
	}
	err := s.ledger.Complete(ctx, s.trackId, s.userId, jobFenceFrom(ctx), completion)
	if errors.Is(err, ports.ErrTrackNotFound) {
		return afterUpdate{}, errors.New("track not found for update")
	}
	return afterUpdate{}, err
}

func (s *UpdateTrackStep) Rollback(ctx context.Context, _ *AcquisitionContext) error {
	err := s.ledger.Revert(ctx, s.trackId, s.userId, jobFenceFrom(ctx))
	if errors.Is(err, ports.ErrIllegalTransition) {
		return nil
	}
	return err
}

type commitCheckContextKey struct{}

type commitCheck func(ctx context.Context) error

func withCommitCheck(ctx context.Context, check commitCheck) context.Context {
	return context.WithValue(ctx, commitCheckContextKey{}, check)
}

func verifyCommitAllowed(ctx context.Context) error {
	check, ok := ctx.Value(commitCheckContextKey{}).(commitCheck)
	if !ok {
		return nil
	}
	return check(ctx)
}
