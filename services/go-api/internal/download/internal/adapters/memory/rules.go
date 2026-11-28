package memory

import (
	"altune/go-api/internal/download/internal/ports"
	"fmt"
)

type Rules struct{}

var _ ports.StatusRules = Rules{}

func (Rules) Next(from ports.TrackStatus, event ports.TrackEvent) (ports.TrackStatus, error) {
	legal := map[ports.TrackEvent]struct {
		from []ports.TrackStatus
		to   ports.TrackStatus
	}{
		ports.EventMarkReady:       {[]ports.TrackStatus{ports.TrackPending, ports.TrackReady, ports.TrackFailed}, ports.TrackReady},
		ports.EventReplaceAudio:    {[]ports.TrackStatus{ports.TrackReady}, ports.TrackReady},
		ports.EventMarkFailed:      {[]ports.TrackStatus{ports.TrackPending, ports.TrackReady}, ports.TrackFailed},
		ports.EventFailPending:     {[]ports.TrackStatus{ports.TrackPending}, ports.TrackFailed},
		ports.EventRevertToPending: {[]ports.TrackStatus{ports.TrackReady, ports.TrackFailed}, ports.TrackPending},
	}[event]
	for _, s := range legal.from {
		if s == from {
			return legal.to, nil
		}
	}
	return from, fmt.Errorf("%w: event %d from %s", ports.ErrIllegalTransition, event, from.Wire())
}

func (Rules) Claimable() []ports.TrackStatus { return []ports.TrackStatus{ports.TrackPending} }

func (Rules) Retryable() []ports.TrackStatus { return []ports.TrackStatus{ports.TrackFailed} }

func (Rules) Upgradable() []ports.TrackStatus { return []ports.TrackStatus{ports.TrackReady} }
