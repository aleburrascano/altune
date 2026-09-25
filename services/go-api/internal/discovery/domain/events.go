package domain

import (
	"altune/go-api/internal/shared"
	"time"
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
	EventTypeSearchFailed
	EventTypeSearchDegraded
	EventTypePlaybackHealth
	EventTypeDiscographyObserved
	EventTypeDetailHealth
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
	EventTypeSearchFailed:    "search_failed",
	EventTypeSearchDegraded:  "search_degraded",
	EventTypePlaybackHealth:  "playback_health",
	// detail_health is the enrichment/discovery counterpart of playback_health:
	// one aggregate per-provider outcome tally per client batch.
	EventTypeDetailHealth: "detail_health",
	// discography_observed is a server-emitted structural-quality signal (the
	// per-release cross-provider disagreement computed at the artist-content
	// merge). It is deliberately absent from ClientSubmittable below so no
	// client can forge quality data.
	EventTypeDiscographyObserved: "discography_observed",
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
		EventTypeCompleted, EventTypeLibraryAdd, EventTypeWrongAlbum, EventTypeSearchFailed,
		EventTypeSearchDegraded, EventTypePlaybackHealth, EventTypeDetailHealth:
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

// The payload keys that cross the Go/SQL seam: written or validated here in Go
// and read back by the event SQL. Naming each once is what keeps a writer and a
// reader from drifting apart. The values are the pinned wire shape of rows
// already persisted — the identifiers may move, the strings may not.
const (
	PayloadKeyZeroResult         = "zero_result"
	PayloadKeyTailNoiseTop5      = "tail_noise_top5"
	PayloadKeyResultSignature    = "result_signature"
	PayloadKeySessionId          = "session_id"
	PayloadKeyShownSignatures    = "shown_signatures"
	PayloadKeyDwellMs            = "dwell_ms"
	PayloadKeyArtistRef          = "artist_ref"
	PayloadKeyReleases           = "releases"
	PayloadKeySingleProvider     = "single_provider"
	PayloadKeySingleProviderNoId = "single_provider_no_id"
	PayloadKeyProviderCounts     = "provider_counts"
)

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
