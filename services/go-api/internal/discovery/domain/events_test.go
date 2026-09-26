package domain

import "testing"

func TestEventType_String(t *testing.T) {
	tests := []struct {
		et   EventType
		want string
	}{
		{EventTypeSearchPerformed, "search_performed"},
		{EventTypeResultsShown, "results_shown"},
		{EventTypeResultClicked, "result_clicked"},
		{EventTypePlay, "play"},
		{EventTypeSkip, "skip"},
		{EventTypeCompleted, "completed"},
		{EventTypeLibraryAdd, "library_add"},
		{EventTypeWrongAlbum, "wrong_album"},
		{EventTypeSearchFailed, "search_failed"},
		{EventTypeSearchDegraded, "search_degraded"},
		{EventTypePlaybackHealth, "playback_health"},
		{EventTypeDetailHealth, "detail_health"},
		{EventTypeAcquisitionUi, "acquisition_ui"},
		{EventTypeClientError, "client_error"},
		{EventTypeDiscographyObserved, "discography_observed"},
		{EventTypeUnknown, "unknown"},
		{EventType(999), "unknown"},
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

// Every key below is already written into persisted rows and read back by the
// event SQL, so a changed value strands that history rather than renaming it.
func TestPayloadKeysStayPinned(t *testing.T) {
	tests := []struct {
		key  string
		want string
	}{
		{PayloadKeyZeroResult, "zero_result"},
		{PayloadKeyTailNoiseTop5, "tail_noise_top5"},
		{PayloadKeyResultSignature, "result_signature"},
		{PayloadKeySessionId, "session_id"},
		{PayloadKeyShownSignatures, "shown_signatures"},
		{PayloadKeyDwellMs, "dwell_ms"},
		{PayloadKeyArtistRef, "artist_ref"},
		{PayloadKeyReleases, "releases"},
		{PayloadKeySingleProvider, "single_provider"},
		{PayloadKeySingleProviderNoId, "single_provider_no_id"},
		{PayloadKeyProviderCounts, "provider_counts"},
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
		want  EventType
	}{
		{name: "search_performed", input: "search_performed", want: EventTypeSearchPerformed},
		{name: "results_shown", input: "results_shown", want: EventTypeResultsShown},
		{name: "result_clicked", input: "result_clicked", want: EventTypeResultClicked},
		{name: "play", input: "play", want: EventTypePlay},
		{name: "skip", input: "skip", want: EventTypeSkip},
		{name: "completed", input: "completed", want: EventTypeCompleted},
		{name: "library_add", input: "library_add", want: EventTypeLibraryAdd},
		{name: "wrong_album", input: "wrong_album", want: EventTypeWrongAlbum},
		{name: "search_failed", input: "search_failed", want: EventTypeSearchFailed},
		{name: "search_degraded", input: "search_degraded", want: EventTypeSearchDegraded},
		{name: "playback_health", input: "playback_health", want: EventTypePlaybackHealth},
		{name: "detail_health", input: "detail_health", want: EventTypeDetailHealth},
		{name: "acquisition_ui", input: "acquisition_ui", want: EventTypeAcquisitionUi},
		{name: "client_error", input: "client_error", want: EventTypeClientError},
		{name: "discography_observed", input: "discography_observed", want: EventTypeDiscographyObserved},
		{name: "invalid", input: "page_view", want: EventTypeUnknown},
		{name: "empty", input: "", want: EventTypeUnknown},
		{name: "uppercase rejected", input: "Play", want: EventTypeUnknown},
		{name: "sentinel name rejected", input: "unknown", want: EventTypeUnknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got := ParseEventType(tt.input)
			if got != tt.want {
				t.Errorf("ParseEventType(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestParseEventType_RoundTrip(t *testing.T) {
	types := []EventType{
		EventTypeSearchPerformed, EventTypeResultsShown, EventTypeResultClicked,
		EventTypePlay, EventTypeSkip, EventTypeCompleted,
		EventTypeLibraryAdd, EventTypeWrongAlbum, EventTypeSearchFailed,
		EventTypeSearchDegraded, EventTypePlaybackHealth, EventTypeDetailHealth,
		EventTypeAcquisitionUi, EventTypeClientError,
		EventTypeDiscographyObserved,
	}
	for _, et := range types {
		t.Run(et.String(), func(t *testing.T) {
			parsed := ParseEventType(et.String())
			if parsed != et {
				t.Errorf("round-trip: got %v, want %v", parsed, et)
			}
		})
	}
}

func TestEventType_ClientSubmittable(t *testing.T) {
	tests := []struct {
		et   EventType
		want bool
	}{
		{EventTypeUnknown, false},
		{EventTypeSearchPerformed, false},
		{EventTypeResultsShown, true},
		{EventTypeResultClicked, true},
		{EventTypePlay, true},
		{EventTypeSkip, true},
		{EventTypeCompleted, true},
		{EventTypeLibraryAdd, true},
		{EventTypeWrongAlbum, true},
		{EventTypeSearchFailed, true},
		{EventTypeSearchDegraded, true},
		{EventTypePlaybackHealth, true},
		{EventTypeDetailHealth, true},
		{EventTypeAcquisitionUi, true},
		{EventTypeClientError, true},
		// discography_observed is server-emitted only: a client must never be
		// able to forge structural-quality data.
		{EventTypeDiscographyObserved, false},
		{EventType(999), false},
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
