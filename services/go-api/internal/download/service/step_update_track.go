package service

import (
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

type UpdateTrackStep struct {
	ledger  ports.LibraryTracks
	userId  ids.UserId
	trackId ids.TrackId
}

func NewUpdateTrackStep(ledger ports.LibraryTracks, userId ids.UserId, trackId ids.TrackId) *UpdateTrackStep {
	return &UpdateTrackStep{
		ledger:  ledger,
		userId:  userId,
		trackId: trackId,
	}
}

func (s *UpdateTrackStep) Name() StepName { return stepNameUpdateTrack }

func (s *UpdateTrackStep) Execute(ctx context.Context, ac *AcquisitionContext, stored AfterStore) (afterUpdate, error) {
	evidence := ac.CollectEvidence()
	if err := ac.admitFetched(evidence.Grade); err != nil {
		return afterUpdate{}, err
	}
	completion, err := completionOf(ac, stored.Kept, evidence)
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
