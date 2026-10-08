package ports

import (
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
)

type EventSubscriber interface {
	Subscribe(userId shared.UserId) (ch <-chan events.Event, cancel func())
	Replay(userId shared.UserId, afterID uint64) []events.Event
	ResumeGapped(userId shared.UserId, afterID uint64) bool
	HighestIssuedID() uint64
	LatestID(userId shared.UserId) uint64
}
