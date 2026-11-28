package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
)

type UpdateTrackStep struct {
	ledger   ports.LibraryTracks
	evidence ports.EvidenceCodec
	userId   ids.UserId
	trackId  ids.TrackId
}

func NewUpdateTrackStep(ledger ports.LibraryTracks, evidence ports.EvidenceCodec, userId ids.UserId, trackId ids.TrackId) *UpdateTrackStep {
	return &UpdateTrackStep{
		ledger:   ledger,
		evidence: evidence,
		userId:   userId,
		trackId:  trackId,
	}
}

func (s *UpdateTrackStep) Name() StepName { return stepNameUpdateTrack }

func (s *UpdateTrackStep) Execute(ctx context.Context, ac *AcquisitionContext, stored AfterStore) (afterUpdate, error) {
	evidence := ac.CollectEvidence()
	if err := ac.admitFetched(evidence.Grade); err != nil {
		return afterUpdate{}, err
	}
	completion, err := completionOf(s.evidence, ac, stored.Kept, evidence)
	if err != nil {
		return afterUpdate{}, err
	}
	if err := ac.Job.VerifyCommit(ctx); err != nil {
		return afterUpdate{}, fmt.Errorf("%w", err)
	}
	err = s.ledger.Complete(ctx, s.trackId, s.userId, ac.Job.Claim, completion)
	if errors.Is(err, domain.ErrTrackNotFound) {
		return afterUpdate{}, errors.New("track not found for update")
	}
	return afterUpdate{}, err
}

func completionOf(codec ports.EvidenceCodec, ac *AcquisitionContext, kept domain.AudioCandidate, evidence domain.AcquisitionEvidence) (domain.Completion, error) {
	raw, err := codec.Encode(evidence)
	if err != nil {
		return domain.Completion{}, fmt.Errorf("encode acquisition evidence: %w", err)
	}
	isrc, _ := ac.provenISRC()
	return domain.Completion{
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
