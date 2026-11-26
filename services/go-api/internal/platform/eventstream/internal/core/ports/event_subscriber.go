package ports

import (
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
)

type EventSubscriber interface {
	Subscribe(userId ids.UserId) (ch <-chan events.Event, cancel func())
	Replay(userId ids.UserId, afterID uint64) []events.Event
	ResumeGapped(userId ids.UserId, afterID uint64) bool
	HighestIssuedID() uint64
	LatestID(userId ids.UserId) uint64
}
