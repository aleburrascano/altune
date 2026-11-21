package domain_test

import (
	"altune/go-api/internal/discovery/domain"
	"testing"
)

func TestEventType_String(t *testing.T) {
	tests := []struct {
		et   domain.EventType
		want string
	}{
		{domain.EventTypeSearchPerformed, "search_performed"},
		{domain.EventTypeResultsShown, "results_shown"},
		{domain.EventTypeResultClicked, "result_clicked"},
		{domain.EventTypePlay, "play"},
		{domain.EventTypeSkip, "skip"},
		{domain.EventTypeCompleted, "completed"},
		{domain.EventTypeLibraryAdd, "library_add"},
		{domain.EventTypeWrongAlbum, "wrong_album"},
		{domain.EventTypeSearchFailed, "search_failed"},
		{domain.EventTypeSearchDegraded, "search_degraded"},
		{domain.EventTypePlaybackHealth, "playback_health"},
		{domain.EventTypeDetailHealth, "detail_health"},
		{domain.EventTypeAcquisitionUi, "acquisition_ui"},
		{domain.EventTypeClientError, "client_error"},
		{domain.EventTypeDiscographyObserved, "discography_observed"},
		{domain.EventTypeUnknown, "unknown"},
		{domain.EventType(999), "unknown"},
		{domain.EventTypeUserAction, "user_action"},
		{domain.EventTypeFailureShown, "failure_shown"},
		{domain.EventTypeSseReconnect, "sse_reconnect"},
		{domain.EventTypeOutboxFlushFailed, "outbox_flush_failed"},
		{domain.EventTypeDownloadFailed, "download_failed"},
		{domain.EventTypeAuthFailed, "auth_failed"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.et.String()
			if got != tt.want {
				t.Errorf("EventType(%d).String() = %q, want %q", tt.et, got, tt.want)
			}
		})
	}
}

func TestPayloadKeysStayPinned(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{domain.PayloadKeyZeroResult, "zero_result"},
		{domain.PayloadKeyTailNoiseTop5, "tail_noise_top5"},
		{domain.PayloadKeyResultSignature, "result_signature"},
		{domain.PayloadKeySessionId, "session_id"},
		{domain.PayloadKeyShownSignatures, "shown_signatures"},
		{domain.PayloadKeyDwellMs, "dwell_ms"},
		{domain.PayloadKeyArtistRef, "artist_ref"},
		{domain.PayloadKeyReleases, "releases"},
		{domain.PayloadKeySingleProvider, "single_provider"},
		{domain.PayloadKeySingleProviderNoId, "single_provider_no_id"},
		{domain.PayloadKeyProviderCounts, "provider_counts"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if tt.key != tt.want {
				t.Errorf("payload key = %q, want %q", tt.key, tt.want)
			}
		})
	}
}

func TestParseEventType(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  domain.EventType
	}{
		{name: "search_performed", input: "search_performed", want: domain.EventTypeSearchPerformed},
		{name: "results_shown", input: "results_shown", want: domain.EventTypeResultsShown},
		{name: "result_clicked", input: "result_clicked", want: domain.EventTypeResultClicked},
		{name: "play", input: "play", want: domain.EventTypePlay},
		{name: "skip", input: "skip", want: domain.EventTypeSkip},
		{name: "completed", input: "completed", want: domain.EventTypeCompleted},
		{name: "library_add", input: "library_add", want: domain.EventTypeLibraryAdd},
		{name: "wrong_album", input: "wrong_album", want: domain.EventTypeWrongAlbum},
		{name: "search_failed", input: "search_failed", want: domain.EventTypeSearchFailed},
		{name: "search_degraded", input: "search_degraded", want: domain.EventTypeSearchDegraded},
		{name: "playback_health", input: "playback_health", want: domain.EventTypePlaybackHealth},
		{name: "detail_health", input: "detail_health", want: domain.EventTypeDetailHealth},
		{name: "acquisition_ui", input: "acquisition_ui", want: domain.EventTypeAcquisitionUi},
		{name: "client_error", input: "client_error", want: domain.EventTypeClientError},
		{name: "user_action", input: "user_action", want: domain.EventTypeUserAction},
		{name: "failure_shown", input: "failure_shown", want: domain.EventTypeFailureShown},
		{name: "sse_reconnect", input: "sse_reconnect", want: domain.EventTypeSseReconnect},
		{name: "outbox_flush_failed", input: "outbox_flush_failed", want: domain.EventTypeOutboxFlushFailed},
		{name: "download_failed", input: "download_failed", want: domain.EventTypeDownloadFailed},
		{name: "discography_observed", input: "discography_observed", want: domain.EventTypeDiscographyObserved},
		{name: "invalid", input: "page_view", want: domain.EventTypeUnknown},
		{name: "empty", input: "", want: domain.EventTypeUnknown},
		{name: "uppercase rejected", input: "Play", want: domain.EventTypeUnknown},
		{name: "sentinel name rejected", input: "unknown", want: domain.EventTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ParseEventType(tt.input)
			if got != tt.want {
				t.Errorf("ParseEventType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseEventType_RoundTrip(t *testing.T) {
	types := []domain.EventType{
		domain.EventTypeSearchPerformed, domain.EventTypeResultsShown, domain.EventTypeResultClicked,
		domain.EventTypePlay, domain.EventTypeSkip, domain.EventTypeCompleted,
		domain.EventTypeLibraryAdd, domain.EventTypeWrongAlbum, domain.EventTypeSearchFailed,
		domain.EventTypeSearchDegraded, domain.EventTypePlaybackHealth, domain.EventTypeDetailHealth,
		domain.EventTypeAcquisitionUi, domain.EventTypeClientError,
		domain.EventTypeUserAction, domain.EventTypeFailureShown,
		domain.EventTypeSseReconnect, domain.EventTypeOutboxFlushFailed, domain.EventTypeDownloadFailed,
		domain.EventTypeDiscographyObserved, domain.EventTypeScreenView, domain.EventTypePlayStart,
	}
	for _, et := range types {
		t.Run(et.String(), func(t *testing.T) {
			parsed := domain.ParseEventType(et.String())
			if parsed != et {
				t.Errorf("round-trip: got %v, want %v", parsed, et)
			}
		})
	}
}

func TestEventType_ClientSubmittable(t *testing.T) {
	tests := []struct {
		et   domain.EventType
		want bool
	}{
		{domain.EventTypeUnknown, false},
		{domain.EventTypeSearchPerformed, false},
		{domain.EventTypeResultsShown, true},
		{domain.EventTypeResultClicked, true},
		{domain.EventTypePlay, true},
		{domain.EventTypeSkip, true},
		{domain.EventTypeCompleted, true},
		{domain.EventTypeLibraryAdd, true},
		{domain.EventTypeWrongAlbum, true},
		{domain.EventTypeSearchFailed, true},
		{domain.EventTypeSearchDegraded, true},
		{domain.EventTypePlaybackHealth, true},
		{domain.EventTypeDetailHealth, true},
		{domain.EventTypeAcquisitionUi, true},
		{domain.EventTypeClientError, true},
		{domain.EventTypeUserAction, true},
		{domain.EventTypeFailureShown, true},
		{domain.EventTypeSseReconnect, true},
		{domain.EventTypeOutboxFlushFailed, true},
		{domain.EventTypeDownloadFailed, true},
		{domain.EventTypeDiscographyObserved, false},
		{domain.EventTypeAuthFailed, false},
		{domain.EventTypeScreenView, true},
		{domain.EventTypePlayStart, true},
		{domain.EventType(999), false},
	}

	for _, tt := range tests {
		t.Run(tt.et.String(), func(t *testing.T) {
			got := tt.et.ClientSubmittable()
			if got != tt.want {
				t.Errorf("EventType(%d).ClientSubmittable() = %v, want %v", tt.et, got, tt.want)
			}
		})
	}
}
