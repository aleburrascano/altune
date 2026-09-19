package handler_test

import (
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"runtime"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// TestServeEventRates_ReportsDroppedEvents pins #1001: when a burst overruns the
// tap's channel, the drop count must be visible to operators through
// /events/rates instead of being computed and discarded.
func TestServeEventRates_ReportsDroppedEvents(t *testing.T) {
	// One P keeps the feed loop off-CPU while the publisher bursts, so the
	// tap's bounded channel overflows without depending on scheduler luck.
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))

	tap := eventtap.New(events.NewInProcessBus())
	feed := eventtap.NewFeed()
	feedCtx, stopFeed := context.WithCancel(context.Background())
	defer func() {
		stopFeed()
		feed.Shutdown(context.Background())
	}()
	feed.Start(feedCtx, tap)

	r := chi.NewRouter()
	handler.New(nil, logging.NewRingBuffer(8)).WithEventFeed(feed).RegisterData(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	user := shared.NewUserId(uuid.New())
	deadline := time.Now().Add(streamWait)
	for tap.Dropped() == 0 {
		if time.Now().After(deadline) {
			t.Fatalf("tap never dropped an event within %s", streamWait)
		}
		for i := 0; i < 4096; i++ {
			tap.Publish(context.Background(), user, "burst", nil)
		}
	}
	want := tap.Dropped()

	var body struct {
		Rates   map[string]int `json:"rates"`
		Dropped *uint64        `json:"dropped"`
	}
	getEventRates(t, srv, &body)
	if body.Dropped == nil {
		t.Fatal("response has no dropped field")
	}
	t.Logf("/events/rates: dropped=%d rates=%v", *body.Dropped, body.Rates)
	if *body.Dropped != want {
		t.Errorf("dropped = %d, want %d (tap's counter)", *body.Dropped, want)
	}
	if body.Rates["burst"] == 0 {
		t.Errorf("rates = %v, want a non-zero burst count", body.Rates)
	}
}

// TestServeEventRates_NoFeed keeps the response shape stable when the feed is
// not wired: empty rates and zero drops, never a 500.
func TestServeEventRates_NoFeed(t *testing.T) {
	r := chi.NewRouter()
	handler.New(nil, logging.NewRingBuffer(8)).RegisterData(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	var body map[string]json.RawMessage
	getEventRates(t, srv, &body)
	if string(body["dropped"]) != "0" || string(body["rates"]) != "{}" {
		t.Errorf("body = %v, want rates {} and dropped 0", body)
	}
}

// getEventRates GETs /events/rates over real HTTP, requires a 200 and decodes
// the JSON body into out.
func getEventRates(t *testing.T, srv *httptest.Server, out any) {
	t.Helper()
	resp, err := srv.Client().Get(srv.URL + "/events/rates")
	if err != nil || resp == nil {
		t.Fatalf("GET /events/rates: %v", err)
		return
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatalf("decode: %v", err)
	}
}
