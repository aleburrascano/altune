package memory

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"fmt"
)

type Rules struct{}

var _ ports.StatusRules = Rules{}

func (Rules) Next(from domain.TrackStatus, event domain.TrackEvent) (domain.TrackStatus, error) {
	legal := map[domain.TrackEvent]struct {
		from []domain.TrackStatus
		to   domain.TrackStatus
	}{
		domain.EventMarkReady:       {[]domain.TrackStatus{domain.TrackPending, domain.TrackReady, domain.TrackFailed}, domain.TrackReady},
		domain.EventReplaceAudio:    {[]domain.TrackStatus{domain.TrackReady}, domain.TrackReady},
		domain.EventMarkFailed:      {[]domain.TrackStatus{domain.TrackPending, domain.TrackReady}, domain.TrackFailed},
		domain.EventFailPending:     {[]domain.TrackStatus{domain.TrackPending}, domain.TrackFailed},
		domain.EventRevertToPending: {[]domain.TrackStatus{domain.TrackReady, domain.TrackFailed}, domain.TrackPending},
	}[event]
	for _, s := range legal.from {
		if s == from {
			return legal.to, nil
		}
	}
	return from, fmt.Errorf("%w: event %d from %s", domain.ErrIllegalTransition, event, from.Wire())
}

func (Rules) Claimable() []domain.TrackStatus { return []domain.TrackStatus{domain.TrackPending} }

func (Rules) Retryable() []domain.TrackStatus { return []domain.TrackStatus{domain.TrackFailed} }

func (Rules) Upgradable() []domain.TrackStatus { return []domain.TrackStatus{domain.TrackReady} }
