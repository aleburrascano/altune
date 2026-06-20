package domain

import (
	"time"

	"altune/go-api/internal/shared"
)

type SearchPerformed struct {
	OccurredAt time.Time
	UserId     shared.UserId
	Query      string
	QueryNorm  string
}

type ResultClicked struct {
	OccurredAt      time.Time
	UserId          shared.UserId
	QueryNorm       string
	ResultSignature string
	Position        int
	Confidence      Confidence
}

// EventType discriminates telemetry interaction events. Zero value is the
// unknown sentinel so an uninitialized event is never silently valid.
type EventType int

const (
	EventTypeUnknown EventType = iota
	EventTypeSearchPerformed
	EventTypeResultClicked
	EventTypePlay
	EventTypeSkip
	EventTypeCompleted
	EventTypeLibraryAdd
	EventTypeWrongAlbum
)

var eventTypeNames = map[EventType]string{
	EventTypeSearchPerformed: "search_performed",
	EventTypeResultClicked:   "result_clicked",
	EventTypePlay:            "play",
	EventTypeSkip:            "skip",
	EventTypeCompleted:       "completed",
	EventTypeLibraryAdd:      "library_add",
	EventTypeWrongAlbum:      "wrong_album",
}

func (e EventType) String() string {
	if name, ok := eventTypeNames[e]; ok {
		return name
	}
	return "unknown"
}

// ParseEventType maps a wire string to an EventType. Unknown strings return
// EventTypeUnknown so callers can reject them at the boundary.
func ParseEventType(s string) EventType {
	for t, name := range eventTypeNames {
		if name == s {
			return t
		}
	}
	return EventTypeUnknown
}

// InteractionEvent is one captured entry of the telemetry "interaction
// envelope" — append-only, immutable record of something a user (or the
// search itself) did. Payload carries the variable part of the envelope so it
// can grow without a schema migration.
type InteractionEvent struct {
	OccurredAt time.Time
	UserId     shared.UserId
	Type       EventType
	QueryNorm  string
	Payload    map[string]any
}
