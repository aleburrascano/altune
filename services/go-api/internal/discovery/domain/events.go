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

// ClientSubmittable reports whether a client may submit this event type over
// the POST /events path. Only the interaction types are client-allowed;
// search_performed and results_shown are server-emitted envelope events — a
// client minting them could poison the coverage/CTR aggregates.
func (e EventType) ClientSubmittable() bool {
	switch e {
	case EventTypeResultClicked, EventTypePlay, EventTypeSkip,
		EventTypeCompleted, EventTypeLibraryAdd, EventTypeWrongAlbum:
		return true
	}
	return false
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
	// SearchId is the keystone join key: the UUID of the search_performed that
	// produced this event. Empty for events with no originating search (e.g. a
	// play from the library). Stored in the real search_id column, not payload.
	SearchId string
	// EventId is the client-minted idempotency key for label-critical events
	// (library_add, wrong_album) delivered via the outbox. Empty for the
	// fire-and-forget tier. Insert dedups on it so a retry is a no-op.
	EventId string
	// ClientOccurredAt is when the client recorded the event (vs OccurredAt /
	// received_at, when the server got it). Zero for events minted server-side.
	ClientOccurredAt time.Time
	Payload          map[string]any
}
