package domain_test

import (
	"altune/go-api/internal/platform/monitoring/internal/core/domain"
	"altune/go-api/internal/shared/jsoncontract"
	"encoding/json"
	"strings"
	"testing"
)

func TestWire_TelemetryEventTypes(t *testing.T) {
	body, err := json.Marshal(domain.ClientEventTypeNames())
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	jsoncontract.AssertWireExact(t, "telemetry_event_types", body)
}

func TestParseClientEventType_OnlyOperationalTypesAreOurs(t *testing.T) {
	for _, name := range []string{"client_error", "acquisition_ui", "playback_health", "screen_view"} {
		if _, ok := domain.ParseClientEventType(name); !ok {
			t.Errorf("ParseClientEventType(%q) refused an operational type", name)
		}
	}
	for _, name := range []string{"", "results_shown", "play", "skip", "library_add", "search_performed", "auth_failed"} {
		if _, ok := domain.ParseClientEventType(name); ok {
			t.Errorf("ParseClientEventType(%q) accepted a type another module owns", name)
		}
	}
}

func TestClientEvent_Validate(t *testing.T) {
	const eventID = "6f1c2f6e-8f3a-4f0e-9d1c-0a3b6a8c9d10"
	cases := map[string]struct {
		event   domain.ClientEvent
		wantErr string
	}{
		"a bare screen view is fine":               {domain.ClientEvent{Type: domain.ClientEventScreenView}, ""},
		"a client error needs an event id":         {domain.ClientEvent{Type: domain.ClientEventClientError}, "event_id is required"},
		"a client error with an event id is fine":  {domain.ClientEvent{Type: domain.ClientEventClientError, EventId: eventID}, ""},
		"an event id must be a uuid":               {domain.ClientEvent{Type: domain.ClientEventScreenView, EventId: "nope"}, "non-nil UUID"},
		"a nil uuid is refused":                    {domain.ClientEvent{Type: domain.ClientEventScreenView, EventId: "00000000-0000-0000-0000-000000000000"}, "non-nil UUID"},
		"too many payload keys":                    {domain.ClientEvent{Type: domain.ClientEventScreenView, Payload: manyKeys(domain.MaxClientEventPayloadKeys + 1)}, "at most 32 keys"},
		"a payload at the key limit is fine":       {domain.ClientEvent{Type: domain.ClientEventScreenView, Payload: manyKeys(domain.MaxClientEventPayloadKeys)}, ""},
		"a NUL in a payload string is refused":     {domain.ClientEvent{Type: domain.ClientEventScreenView, Payload: map[string]any{"k": "a\x00b"}}, "valid UTF-8 without NUL"},
		"invalid UTF-8 in a nested key is refused": {domain.ClientEvent{Type: domain.ClientEventScreenView, Payload: map[string]any{"k": map[string]any{"\xff": 1}}}, "valid UTF-8 without NUL"},
		"an oversized payload is refused":          {domain.ClientEvent{Type: domain.ClientEventScreenView, Payload: map[string]any{"k": strings.Repeat("x", domain.MaxClientEventPayloadBytes)}}, "at most 8192 bytes"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			err := tc.event.Validate()

			switch {
			case tc.wantErr == "" && err != nil:
				t.Fatalf("Validate() = %v, want nil", err)
			case tc.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tc.wantErr)):
				t.Fatalf("Validate() = %v, want an error mentioning %q", err, tc.wantErr)
			}
		})
	}
}

func manyKeys(n int) map[string]any {
	m := make(map[string]any, n)
	for i := range n {
		m[strings.Repeat("k", i+1)] = i
	}
	return m
}
