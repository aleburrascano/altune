package service

import (
	"altune/go-api/internal/acquisition/internal/core/ports"
	shared "altune/go-api/internal/shared/kernel"
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

func (s *UpdateTrackStep) Execute(ctx context.Context, ac *AcquisitionContext, stored afterStore) (afterUpdate, error) {
	evidence := ac.collectEvidence()
	if err := ac.Replace.Gate.AdmitFetched(evidence.Grade, ac.SourceFormat, ac.ProbedDuration.Seconds()); err != nil {
		return afterUpdate{}, err
	}
	completion, err := completionOf(ac, stored.kept, evidence)
	if err != nil {
		return afterUpdate{}, err
	}
	if err := verifyCommitAllowed(ctx); err != nil {
		return afterUpdate{}, err
	}
	err = s.ledger.Complete(ctx, s.trackId, s.userId, jobFenceFrom(ctx), completion)
	if errors.Is(err, ports.ErrTrackNotFound) {
		return afterUpdate{}, errors.New("track not found for update")
	}
	return afterUpdate{}, err
}

func completionOf(ac *AcquisitionContext, kept ports.AudioCandidate, evidence Evidence) (ports.Completion, error) {
	raw, err := json.Marshal(evidence)
	if err != nil {
		return ports.Completion{}, fmt.Errorf("marshal acquisition evidence: %w", err)
	}
	isrc, _ := ac.provenISRC()
	return ports.Completion{
		AudioRef:           ac.AudioRef,
		ReplaceAudio:       ac.Replace.PreservedRef != "",
		Duration:           ac.StoredLength(),
		SourceURL:          kept.URL,
		Confidence:         ac.Confidence,
		Evidence:           raw,
		ISRC:               isrc,
		Provenance:         ac.Provenance(),
		RejectedSourceKeys: ac.Replace.ExcludeKeys,
		Relabel:            ac.Relabel,
	}, nil
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
