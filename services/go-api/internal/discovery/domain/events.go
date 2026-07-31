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

type EventType int

const (
	EventTypeUnknown EventType = iota
	EventTypeSearchPerformed
	EventTypeResultsShown
	EventTypeResultClicked
	EventTypePlay
	EventTypeSkip
	EventTypeCompleted
	EventTypeLibraryAdd
	EventTypeWrongAlbum
)

var eventTypeNames = map[EventType]string{
	EventTypeSearchPerformed: "search_performed",
	EventTypeResultsShown:    "results_shown",
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

func (e EventType) ClientSubmittable() bool {
	switch e {
	case EventTypeResultsShown, EventTypeResultClicked, EventTypePlay, EventTypeSkip,
		EventTypeCompleted, EventTypeLibraryAdd, EventTypeWrongAlbum:
		return true
	}
	return false
}

func ParseEventType(s string) EventType {
	for t, name := range eventTypeNames {
		if name == s {
			return t
		}
	}
	return EventTypeUnknown
}

type InteractionEvent struct {
	OccurredAt       time.Time
	UserId           shared.UserId
	Type             EventType
	QueryNorm        string
	SearchId         string
	EventId          string
	ClientOccurredAt time.Time
	Payload          map[string]any
}
