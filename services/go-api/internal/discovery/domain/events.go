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
	EventTypeAcquisitionUi
	EventTypeClientError
	EventTypeUserAction
	EventTypeFailureShown
	EventTypeSseReconnect
	EventTypeOutboxFlushFailed
	EventTypeDownloadFailed
)

var eventTypeNames = map[EventType]string{
	EventTypeSearchPerformed:     "search_performed",
	EventTypeResultsShown:        "results_shown",
	EventTypeResultClicked:       "result_clicked",
	EventTypePlay:                "play",
	EventTypeSkip:                "skip",
	EventTypeCompleted:           "completed",
	EventTypeLibraryAdd:          "library_add",
	EventTypeWrongAlbum:          "wrong_album",
	EventTypeSearchFailed:        "search_failed",
	EventTypeSearchDegraded:      "search_degraded",
	EventTypePlaybackHealth:      "playback_health",
	EventTypeDetailHealth:        "detail_health",
	EventTypeAcquisitionUi:       "acquisition_ui",
	EventTypeClientError:         "client_error",
	EventTypeUserAction:          "user_action",
	EventTypeFailureShown:        "failure_shown",
	EventTypeDiscographyObserved: "discography_observed",
	EventTypeSseReconnect:        "sse_reconnect",
	EventTypeOutboxFlushFailed:   "outbox_flush_failed",
	EventTypeDownloadFailed:      "download_failed",
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
		EventTypeSearchDegraded, EventTypePlaybackHealth, EventTypeDetailHealth,
		EventTypeAcquisitionUi, EventTypeClientError, EventTypeUserAction, EventTypeFailureShown,
		EventTypeSseReconnect, EventTypeOutboxFlushFailed, EventTypeDownloadFailed:
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
