package domain_test

import (
	"altune/go-api/internal/search/internal/domain"
	"testing"
)

func TestInteractionType_String(t *testing.T) {
	tests := []struct {
		et   domain.InteractionType
		want string
	}{
		{domain.InteractionTypeSearchPerformed, "search_performed"},
		{domain.InteractionTypeResultsShown, "results_shown"},
		{domain.InteractionTypeResultClicked, "result_clicked"},
		{domain.InteractionTypePlay, "play"},
		{domain.InteractionTypeSkip, "skip"},
		{domain.InteractionTypeCompleted, "completed"},
		{domain.InteractionTypeLibraryAdd, "library_add"},
		{domain.InteractionTypeWrongAlbum, "wrong_album"},
		{domain.InteractionTypeSearchFailed, "search_failed"},
		{domain.InteractionTypeSearchDegraded, "search_degraded"},
		{domain.InteractionTypePlaybackHealth, "playback_health"},
		{domain.InteractionTypeDetailHealth, "detail_health"},
		{domain.InteractionTypeAcquisitionUi, "acquisition_ui"},
		{domain.InteractionTypeClientError, "client_error"},
		{domain.InteractionTypeDiscographyObserved, "discography_observed"},
		{domain.InteractionTypeUnknown, "unknown"},
		{domain.InteractionType(999), "unknown"},
		{domain.InteractionTypeUserAction, "user_action"},
		{domain.InteractionTypeFailureShown, "failure_shown"},
		{domain.InteractionTypeSseReconnect, "sse_reconnect"},
		{domain.InteractionTypeOutboxFlushFailed, "outbox_flush_failed"},
		{domain.InteractionTypeDownloadFailed, "download_failed"},
		{domain.InteractionTypeAuthFailed, "auth_failed"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := tt.et.String()
			if got != tt.want {
				t.Errorf("InteractionType(%d).String() = %q, want %q", tt.et, got, tt.want)
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

func TestParseInteractionType(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  domain.InteractionType
	}{
		{name: "search_performed", input: "search_performed", want: domain.InteractionTypeSearchPerformed},
		{name: "results_shown", input: "results_shown", want: domain.InteractionTypeResultsShown},
		{name: "result_clicked", input: "result_clicked", want: domain.InteractionTypeResultClicked},
		{name: "play", input: "play", want: domain.InteractionTypePlay},
		{name: "skip", input: "skip", want: domain.InteractionTypeSkip},
		{name: "completed", input: "completed", want: domain.InteractionTypeCompleted},
		{name: "library_add", input: "library_add", want: domain.InteractionTypeLibraryAdd},
		{name: "wrong_album", input: "wrong_album", want: domain.InteractionTypeWrongAlbum},
		{name: "search_failed", input: "search_failed", want: domain.InteractionTypeSearchFailed},
		{name: "search_degraded", input: "search_degraded", want: domain.InteractionTypeSearchDegraded},
		{name: "playback_health", input: "playback_health", want: domain.InteractionTypePlaybackHealth},
		{name: "detail_health", input: "detail_health", want: domain.InteractionTypeDetailHealth},
		{name: "acquisition_ui", input: "acquisition_ui", want: domain.InteractionTypeAcquisitionUi},
		{name: "client_error", input: "client_error", want: domain.InteractionTypeClientError},
		{name: "user_action", input: "user_action", want: domain.InteractionTypeUserAction},
		{name: "failure_shown", input: "failure_shown", want: domain.InteractionTypeFailureShown},
		{name: "sse_reconnect", input: "sse_reconnect", want: domain.InteractionTypeSseReconnect},
		{name: "outbox_flush_failed", input: "outbox_flush_failed", want: domain.InteractionTypeOutboxFlushFailed},
		{name: "download_failed", input: "download_failed", want: domain.InteractionTypeDownloadFailed},
		{name: "discography_observed", input: "discography_observed", want: domain.InteractionTypeDiscographyObserved},
		{name: "invalid", input: "page_view", want: domain.InteractionTypeUnknown},
		{name: "empty", input: "", want: domain.InteractionTypeUnknown},
		{name: "uppercase rejected", input: "Play", want: domain.InteractionTypeUnknown},
		{name: "sentinel name rejected", input: "unknown", want: domain.InteractionTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := domain.ParseInteractionType(tt.input)
			if got != tt.want {
				t.Errorf("ParseInteractionType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseInteractionType_RoundTrip(t *testing.T) {
	types := []domain.InteractionType{
		domain.InteractionTypeSearchPerformed, domain.InteractionTypeResultsShown, domain.InteractionTypeResultClicked,
		domain.InteractionTypePlay, domain.InteractionTypeSkip, domain.InteractionTypeCompleted,
		domain.InteractionTypeLibraryAdd, domain.InteractionTypeWrongAlbum, domain.InteractionTypeSearchFailed,
		domain.InteractionTypeSearchDegraded, domain.InteractionTypePlaybackHealth, domain.InteractionTypeDetailHealth,
		domain.InteractionTypeAcquisitionUi, domain.InteractionTypeClientError,
		domain.InteractionTypeUserAction, domain.InteractionTypeFailureShown,
		domain.InteractionTypeSseReconnect, domain.InteractionTypeOutboxFlushFailed, domain.InteractionTypeDownloadFailed,
		domain.InteractionTypeDiscographyObserved, domain.InteractionTypeScreenView, domain.InteractionTypePlayStart,
	}
	for _, et := range types {
		t.Run(et.String(), func(t *testing.T) {
			parsed := domain.ParseInteractionType(et.String())
			if parsed != et {
				t.Errorf("round-trip: got %v, want %v", parsed, et)
			}
		})
	}
}

func TestInteractionType_ClientSubmittable(t *testing.T) {
	tests := []struct {
		et   domain.InteractionType
		want bool
	}{
		{domain.InteractionTypeUnknown, false},
		{domain.InteractionTypeSearchPerformed, false},
		{domain.InteractionTypeResultsShown, true},
		{domain.InteractionTypeResultClicked, true},
		{domain.InteractionTypePlay, true},
		{domain.InteractionTypeSkip, true},
		{domain.InteractionTypeCompleted, true},
		{domain.InteractionTypeLibraryAdd, true},
		{domain.InteractionTypeWrongAlbum, true},
		{domain.InteractionTypeSearchFailed, true},
		{domain.InteractionTypeSearchDegraded, true},
		{domain.InteractionTypePlaybackHealth, true},
		{domain.InteractionTypeDetailHealth, true},
		{domain.InteractionTypeAcquisitionUi, true},
		{domain.InteractionTypeClientError, true},
		{domain.InteractionTypeUserAction, true},
		{domain.InteractionTypeFailureShown, true},
		{domain.InteractionTypeSseReconnect, true},
		{domain.InteractionTypeOutboxFlushFailed, true},
		{domain.InteractionTypeDownloadFailed, true},
		{domain.InteractionTypeDiscographyObserved, false},
		{domain.InteractionTypeAuthFailed, false},
		{domain.InteractionTypeScreenView, true},
		{domain.InteractionTypePlayStart, true},
		{domain.InteractionType(999), false},
	}

	for _, tt := range tests {
		t.Run(tt.et.String(), func(t *testing.T) {
			got := tt.et.ClientSubmittable()
			if got != tt.want {
				t.Errorf("InteractionType(%d).ClientSubmittable() = %v, want %v", tt.et, got, tt.want)
			}
		})
	}
}
