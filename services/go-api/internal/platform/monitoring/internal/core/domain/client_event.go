package domain

import (
	"altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/ids"
	"encoding/json"
	"fmt"
	"time"
	"unicode/utf8"
)

type ClientEventType string

const (
	ClientEventPlaybackHealth    ClientEventType = "playback_health"
	ClientEventDetailHealth      ClientEventType = "detail_health"
	ClientEventAcquisitionUI     ClientEventType = "acquisition_ui"
	ClientEventClientError       ClientEventType = "client_error"
	ClientEventUserAction        ClientEventType = "user_action"
	ClientEventFailureShown      ClientEventType = "failure_shown"
	ClientEventSseReconnect      ClientEventType = "sse_reconnect"
	ClientEventOutboxFlushFailed ClientEventType = "outbox_flush_failed"
	ClientEventDownloadFailed    ClientEventType = "download_failed"
	ClientEventScreenView        ClientEventType = "screen_view"
	ClientEventPlayStart         ClientEventType = "play_start"
)

var clientEventTypes = map[string]ClientEventType{
	string(ClientEventPlaybackHealth):    ClientEventPlaybackHealth,
	string(ClientEventDetailHealth):      ClientEventDetailHealth,
	string(ClientEventAcquisitionUI):     ClientEventAcquisitionUI,
	string(ClientEventClientError):       ClientEventClientError,
	string(ClientEventUserAction):        ClientEventUserAction,
	string(ClientEventFailureShown):      ClientEventFailureShown,
	string(ClientEventSseReconnect):      ClientEventSseReconnect,
	string(ClientEventOutboxFlushFailed): ClientEventOutboxFlushFailed,
	string(ClientEventDownloadFailed):    ClientEventDownloadFailed,
	string(ClientEventScreenView):        ClientEventScreenView,
	string(ClientEventPlayStart):         ClientEventPlayStart,
}

func ParseClientEventType(s string) (ClientEventType, bool) {
	t, ok := clientEventTypes[s]
	return t, ok
}

func (t ClientEventType) RequiresEventID() bool {
	switch t {
	case ClientEventAcquisitionUI, ClientEventClientError:
		return true
	case ClientEventPlaybackHealth, ClientEventDetailHealth, ClientEventUserAction, ClientEventFailureShown,
		ClientEventSseReconnect, ClientEventOutboxFlushFailed, ClientEventDownloadFailed, ClientEventScreenView,
		ClientEventPlayStart:
		return false
	}
	return false
}

type ClientEvent struct {
	UserId           ids.UserId
	Type             ClientEventType
	EventId          string
	CorrelationId    string
	ClientOccurredAt time.Time
	OccurredAt       time.Time
	Payload          map[string]any
}

const (
	MaxClientEventPayloadBytes = 8 << 10
	MaxClientEventPayloadKeys  = 32
)

func NewInvalidClientEventError(msg string) *apierror.CodedError {
	return apierror.NewError(apierror.CodeDiscoveryInvalidEvent, msg)
}

func (e ClientEvent) Validate() error {
	if e.EventId == "" {
		if e.Type.RequiresEventID() {
			return NewInvalidClientEventError(fmt.Sprintf("event_id is required for %q events", e.Type))
		}
	} else if id, err := ids.ParseEntityId(e.EventId); err != nil || id.IsZero() {
		return NewInvalidClientEventError("event_id must be a non-nil UUID")
	}
	if len(e.Payload) > MaxClientEventPayloadKeys {
		return NewInvalidClientEventError(fmt.Sprintf("payload must hold at most %d keys", MaxClientEventPayloadKeys))
	}
	if !storable(e.Payload) {
		return NewInvalidClientEventError("payload strings must be valid UTF-8 without NUL")
	}
	stored, err := json.Marshal(e.Payload)
	if err != nil {
		return NewInvalidClientEventError("payload must be JSON-serializable")
	}
	if len(stored) > MaxClientEventPayloadBytes {
		return NewInvalidClientEventError(fmt.Sprintf("payload must serialize to at most %d bytes", MaxClientEventPayloadBytes))
	}
	return nil
}

func storable(v any) bool {
	switch t := v.(type) {
	case string:
		return utf8.ValidString(t) && !containsNUL(t)
	case map[string]any:
		for k, item := range t {
			if !storable(k) || !storable(item) {
				return false
			}
		}
	case []any:
		for _, item := range t {
			if !storable(item) {
				return false
			}
		}
	}
	return true
}

func containsNUL(s string) bool {
	for i := range len(s) {
		if s[i] == 0 {
			return true
		}
	}
	return false
}
