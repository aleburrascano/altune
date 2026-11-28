package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/shared/ids"
	"context"
	"errors"
	"fmt"
)

type TrackLedger struct {
	library ports.TrackLibrary
	state   ports.UpgradeState
}

var _ ports.LibraryTracks = (*TrackLedger)(nil)

var errLedgerPortMissing = errors.New("download: the track ledger needs the library half and the upgrade state, or rejected source keys are silently dropped")

func NewTrackLedger(library ports.TrackLibrary, state ports.UpgradeState) (*TrackLedger, error) {
	if library == nil || state == nil {
		return nil, errLedgerPortMissing
	}
	return &TrackLedger{library: library, state: state}, nil
}

func (l *TrackLedger) Get(ctx context.Context, id ids.TrackId, userId ids.UserId) (*domain.TrackSnapshot, error) {
	snapshot, err := l.library.Get(ctx, id, userId)
	if err != nil {
		return nil, fmt.Errorf("get track: %w", err)
	}
	state, err := l.state.State(ctx, id)
	if err != nil {
		return nil, fmt.Errorf("read upgrade state: %w", err)
	}
	snapshot.RejectedSourceKeys = state.RejectedSourceKeys
	if snapshot.Evidence, err = domain.MergeUpgradeEvidence(snapshot.Evidence, state.Evidence); err != nil {
		return nil, fmt.Errorf("merge upgrade evidence: %w", err)
	}
	return snapshot, nil
}

func (l *TrackLedger) Complete(ctx context.Context, id ids.TrackId, userId ids.UserId, fence domain.ClaimNumber, c domain.Completion) error {
	if err := l.settleUpgradeState(ctx, id, userId, fence, c); err != nil {
		return err
	}
	if err := l.library.Complete(ctx, id, userId, fence, c); err != nil {
		return fmt.Errorf("complete track: %w", err)
	}
	return nil
}

func (l *TrackLedger) settleUpgradeState(ctx context.Context, id ids.TrackId, userId ids.UserId, fence domain.ClaimNumber, c domain.Completion) error {
	evidence := domain.KeepEvidence
	if l.library.EvidenceAccepted(c) {
		evidence = domain.ResetEvidence
	}
	if len(c.RejectedSourceKeys) == 0 && evidence == domain.KeepEvidence {
		return nil
	}
	if _, err := l.library.Get(ctx, id, userId); err != nil {
		return fmt.Errorf("get track: %w", err)
	}
	if err := l.state.SettleAcquisition(ctx, id, fence, c.RejectedSourceKeys, evidence); err != nil {
		return fmt.Errorf("settle upgrade state: %w", err)
	}
	return nil
}

func (l *TrackLedger) Fail(ctx context.Context, id ids.TrackId, userId ids.UserId, fence domain.ClaimNumber, reason string) error {
	if err := l.library.Fail(ctx, id, userId, fence, reason); err != nil {
		return fmt.Errorf("fail track: %w", err)
	}
	return nil
}

func (l *TrackLedger) RevertAt(ctx context.Context, snapshot domain.TrackSnapshot) error {
	if err := l.library.RevertAt(ctx, snapshot); err != nil {
		return fmt.Errorf("revert track: %w", err)
	}
	return nil
}

func (l *TrackLedger) AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID ids.TrackId) (bool, error) {
	inUse, err := l.library.AudioRefInUse(ctx, audioRef, excludeTrackID)
	if err != nil {
		return false, fmt.Errorf("audio ref in use: %w", err)
	}
	return inUse, nil
}

func (l *TrackLedger) Next(from domain.TrackStatus, event domain.TrackEvent) (domain.TrackStatus, error) {
	next, err := l.library.Next(from, event)
	if err != nil {
		return next, fmt.Errorf("next status: %w", err)
	}
	return next, nil
}

func (l *TrackLedger) Claimable() []domain.TrackStatus { return l.library.Claimable() }

func (l *TrackLedger) Retryable() []domain.TrackStatus { return l.library.Retryable() }

func (l *TrackLedger) Upgradable() []domain.TrackStatus { return l.library.Upgradable() }

func (l *TrackLedger) Requeue(ctx context.Context, id ids.TrackId) error {
	if err := l.library.Requeue(ctx, id); err != nil {
		return fmt.Errorf("requeue: %w", err)
	}
	return nil
}
