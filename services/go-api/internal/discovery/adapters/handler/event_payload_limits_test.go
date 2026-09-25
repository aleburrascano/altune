package handler

import (
	"net/http"
	"strings"
	"testing"
)

// eventBodyWithJunk is one event carrying junkBytes of payload, the shape an
// authenticated caller would use to park rows in the events table, which are
// kept 30 to 90 days.
func eventBodyWithJunk(t *testing.T, junkBytes int) string {
	t.Helper()
	return discJsonBody(t, map[string]any{
		"type":    "play",
		"payload": map[string]any{"junk": strings.Repeat("x", junkBytes)},
	}).String()
}

func TestRecordEvent_OversizedPayloadsAreNeverStored(t *testing.T) {
	store := &recordingEventStore{}
	router := buildEventRouter(store)
	body := eventBodyWithJunk(t, 900<<10)

	for i := range 100 {
		rec := discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader(body))
		if rec.Code != http.StatusBadRequest && rec.Code != http.StatusTooManyRequests {
			t.Fatalf("request %d: status = %d, want 400 or 429 (body: %s)", i+1, rec.Code, rec.Body.String())
		}
	}

	if len(store.events) != 0 {
		t.Fatalf("store holds %d oversized events, want none", len(store.events))
	}
}

func TestRecordEvent_PayloadOverTheCapAnswersWithTheInvalidEventCode(t *testing.T) {
	router := buildEventRouter(&recordingEventStore{})

	rec := discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader(eventBodyWithJunk(t, 16<<10)))

	assertErrorCode(t, rec, http.StatusBadRequest, "discovery.invalid_event")
}

// A body past the route's own cap is refused before it is decoded, so it
// answers for the body rather than for the payload inside it.
func TestRecordEvent_BodyPastTheRouteCapIsRejectedUndecoded(t *testing.T) {
	router := buildEventRouter(&recordingEventStore{})

	rec := discServe(t, router, http.MethodPost, "/discovery/events", strings.NewReader(eventBodyWithJunk(t, 900<<10)))

	assertErrorCode(t, rec, http.StatusBadRequest, "discovery.invalid_body")
}
