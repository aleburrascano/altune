package domain

import (
	"altune/go-api/internal/shared/ids"
	"time"
)

type InteractionType int

const (
	InteractionTypeUnknown InteractionType = iota
	InteractionTypeSearchPerformed
	InteractionTypeResultsShown
	InteractionTypeResultClicked
	InteractionTypePlay
	InteractionTypeSkip
	InteractionTypeCompleted
	InteractionTypeLibraryAdd
	InteractionTypeWrongAlbum
	InteractionTypeSearchFailed
	InteractionTypeSearchDegraded
	InteractionTypePlaybackHealth
	InteractionTypeDiscographyObserved
	InteractionTypeDetailHealth
	InteractionTypeAcquisitionUi
	InteractionTypeClientError
	InteractionTypeUserAction
	InteractionTypeFailureShown
	InteractionTypeSseReconnect
	InteractionTypeOutboxFlushFailed
	InteractionTypeDownloadFailed
	InteractionTypeAuthFailed
	InteractionTypeScreenView
	InteractionTypePlayStart
)

var interactionTypeNames = map[InteractionType]string{
	InteractionTypeSearchPerformed:     "search_performed",
	InteractionTypeResultsShown:        "results_shown",
	InteractionTypeResultClicked:       "result_clicked",
	InteractionTypePlay:                "play",
	InteractionTypeSkip:                "skip",
	InteractionTypeCompleted:           "completed",
	InteractionTypeLibraryAdd:          "library_add",
	InteractionTypeWrongAlbum:          "wrong_album",
	InteractionTypeSearchFailed:        "search_failed",
	InteractionTypeSearchDegraded:      "search_degraded",
	InteractionTypePlaybackHealth:      "playback_health",
	InteractionTypeDetailHealth:        "detail_health",
	InteractionTypeAcquisitionUi:       "acquisition_ui",
	InteractionTypeClientError:         "client_error",
	InteractionTypeUserAction:          "user_action",
	InteractionTypeFailureShown:        "failure_shown",
	InteractionTypeDiscographyObserved: "discography_observed",
	InteractionTypeSseReconnect:        "sse_reconnect",
	InteractionTypeOutboxFlushFailed:   "outbox_flush_failed",
	InteractionTypeDownloadFailed:      "download_failed",
	InteractionTypeAuthFailed:          "auth_failed",
	InteractionTypeScreenView:          "screen_view",
	InteractionTypePlayStart:           "play_start",
}

func (e InteractionType) String() string {
	if name, ok := interactionTypeNames[e]; ok {
		return name
	}
	return "unknown"
}

func (e InteractionType) ClientSubmittable() bool {
	switch e {
	case InteractionTypeResultsShown, InteractionTypeResultClicked, InteractionTypePlay, InteractionTypeSkip,
		InteractionTypeCompleted, InteractionTypeLibraryAdd, InteractionTypeWrongAlbum, InteractionTypeSearchFailed,
		InteractionTypeSearchDegraded, InteractionTypePlaybackHealth, InteractionTypeDetailHealth,
		InteractionTypeAcquisitionUi, InteractionTypeClientError, InteractionTypeUserAction, InteractionTypeFailureShown,
		InteractionTypeSseReconnect, InteractionTypeOutboxFlushFailed, InteractionTypeDownloadFailed,
		InteractionTypeScreenView, InteractionTypePlayStart:
		return true
	case InteractionTypeUnknown, InteractionTypeSearchPerformed, InteractionTypeDiscographyObserved, InteractionTypeAuthFailed:
		return false
	}
	return false
}

func ParseInteractionType(s string) InteractionType {
	for t, name := range interactionTypeNames {
		if name == s {
			return t
		}
	}
	return InteractionTypeUnknown
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
	UserId           ids.UserId
	Type             InteractionType
	QueryNorm        string
	SearchId         string
	EventId          string
	CorrelationId    string
	ClientOccurredAt time.Time
	Payload          map[string]any
}
