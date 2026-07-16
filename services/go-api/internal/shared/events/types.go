package events

import (
	"time"

	"altune/go-api/internal/shared"
)

type Event struct {
	ID        uint64         `json:"id"`
	Type      string         `json:"type"`
	UserID    shared.UserId  `json:"-"`
	Payload   map[string]any `json:"payload"`
	Timestamp time.Time      `json:"timestamp"`
}

type Publisher interface {
	Publish(userId shared.UserId, eventType string, payload map[string]any)
}

type Subscriber interface {
	Subscribe(userId shared.UserId) (ch <-chan Event, cancel func())
	Replay(userId shared.UserId, afterID uint64) []Event
}
