package handler

import (
	"net/http"
	"testing"
)

// mobileOutboxEventID is the shape apps/mobile/src/shared/telemetry/outbox.ts
// makeEventId mints for every label-critical event: a lower-case RFC 4122 v4.
const mobileOutboxEventID = "3f2b8c1e-9a4d-4e6f-b1c2-7d8e9f0a1b2c"

// Regression for #1093: a label-critical event without a parseable event_id
// used to be stored with a NULL event_id, which the partial unique index never
// dedups, so a retry or double-fire inserted a second row. Every such request
// is now rejected before it reaches the store.
func TestHandleRecordEvent_LabelCriticalRequiresValidEventID(t *testing.T) {
	cases := []struct {
		name    string
		eventID any
	}{
		{"missing", nil},
		{"empty", ""},
		{"not a uuid", "retry-1"},
		{"truncated uuid", "3f2b8c1e-9a4d-4e6f-b1c2"},
		{"nil uuid", "00000000-0000-0000-0000-000000000000"},
	}
	for _, eventType := range []string{"library_add", "wrong_album"} {
		for _, tc := range cases {
			t.Run(eventType+"/"+tc.name, func(t *testing.T) {
				store := &recordingEventStore{}
				router := buildEventRouter(store)
				body := map[string]any{"type": eventType, "payload": map[string]any{"result_signature": "sig"}}
				if tc.eventID != nil {
					body["event_id"] = tc.eventID
				}

				for range 2 {
					rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))
					discAssertStatus(t, rec, http.StatusBadRequest)
				}
				if got := len(store.events); got != 0 {
					t.Errorf("stored %d events, want 0 (an un-dedupable critical event must not be stored)", got)
				}
			})
		}
	}
}

func TestHandleRecordEvent_MalformedEventIDRejectedForEveryType(t *testing.T) {
	for _, eventType := range []string{"play", "skip", "completed", "result_clicked", "results_shown", "playback_health"} {
		t.Run(eventType, func(t *testing.T) {
			store := &recordingEventStore{}
			router := buildEventRouter(store)
			body := map[string]any{"type": eventType, "event_id": "not-a-uuid"}

			rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, body))

			discAssertStatus(t, rec, http.StatusBadRequest)
			if got := len(store.events); got != 0 {
				t.Errorf("stored %d events, want 0", got)
			}
		})
	}
}

// The mobile client sends play/skip/completed and the other fire-and-forget
// events through useRecordEvent with no event_id at all, and only
// library_add/wrong_album through the outbox with a minted one. Every shape a
// real client sends must still be accepted, or its telemetry is silently lost.
func TestHandleRecordEvent_AcceptsEveryRealClientShape(t *testing.T) {
	cases := []struct {
		name string
		body map[string]any
	}{
		{"outbox library_add", map[string]any{
			"type": "library_add", "event_id": mobileOutboxEventID,
			"client_occurred_at": "2026-09-15T10:00:00.000Z",
			"payload":            map[string]any{"result_signature": "sig", "session_id": "s"},
		}},
		{"outbox wrong_album", map[string]any{
			"type": "wrong_album", "event_id": mobileOutboxEventID,
			"client_occurred_at": "2026-09-15T10:00:00.000Z",
			"payload":            map[string]any{"result_signature": "sig", "session_id": "s"},
		}},
		{"upper-case uuid", map[string]any{"type": "library_add", "event_id": "3F2B8C1E-9A4D-4E6F-B1C2-7D8E9F0A1B2C"}},
		{"fire-and-forget play without event_id", map[string]any{"type": "play", "payload": map[string]any{"session_id": "s"}}},
		{"fire-and-forget skip without event_id", map[string]any{"type": "skip", "payload": map[string]any{"dwell_ms": 1200}}},
		{"fire-and-forget completed without event_id", map[string]any{"type": "completed"}},
		{"play with a valid event_id", map[string]any{"type": "play", "event_id": mobileOutboxEventID}},
		{"results_shown without event_id", map[string]any{"type": "results_shown"}},
		{"search_failed without event_id", map[string]any{"type": "search_failed", "payload": map[string]any{"source": "search"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := &recordingEventStore{}
			router := buildEventRouter(store)

			rec := discServe(t, router, http.MethodPost, "/discovery/events", discJsonBody(t, tc.body))

			discAssertStatus(t, rec, http.StatusNoContent)
			if got := len(store.events); got != 1 {
				t.Errorf("stored %d events, want 1", got)
			}
		})
	}
}
