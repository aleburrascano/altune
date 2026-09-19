package handler_test

import (
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"io"
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

// TestEventRoutes_ReportADeadFeed pins #2005: a feed that is missing, or whose
// Start could not subscribe, records nothing and streams nothing. Both routes
// have to say so with a branchable code, because a 200 with empty rates and an
// empty stream is what a healthy, quiet system looks like.
func TestEventRoutes_ReportADeadFeed(t *testing.T) {
	deadFeeds := []struct {
		name string
		feed *eventtap.Feed
	}{
		{"feed not wired", nil},
		{"feed could not subscribe", unsubscribedFeed(t)},
	}

	for _, dead := range deadFeeds {
		for _, path := range []string{"/events/rates", "/events/stream"} {
			t.Run(dead.name+" "+path, func(t *testing.T) {
				srv := eventRoutes(t, dead.feed)

				status, body := getEventRoute(t, srv, path)

				if status != http.StatusServiceUnavailable {
					t.Fatalf("GET %s status = %d, want 503 (body %s)", path, status, body)
				}
				var resp struct {
					Detail string `json:"detail"`
					Code   string `json:"code"`
				}
				if err := json.Unmarshal([]byte(body), &resp); err != nil {
					t.Fatalf("decode body %q: %v", body, err)
				}
				if resp.Code != "admin.event_feed_unavailable" {
					t.Errorf("code = %q, want admin.event_feed_unavailable (body %s)", resp.Code, body)
				}
				if resp.Detail == "" {
					t.Errorf("error response has empty detail: %s", body)
				}
			})
		}
	}
}

// unsubscribedFeed builds the dead feed of #2005: the tap already has its one
// allowed consumer, so the feed's Start fails to subscribe and drains nothing.
func unsubscribedFeed(t *testing.T) *eventtap.Feed {
	t.Helper()
	tap := eventtap.New(events.NewInProcessBus())
	_, releaseTap, err := tap.SubscribeAll()
	if err != nil {
		t.Fatalf("occupy the tap: %v", err)
	}
	t.Cleanup(releaseTap)

	feed := eventtap.NewFeed()
	feed.Start(context.Background(), tap)
	return feed
}

// eventRoutes serves the admin data routes backed by feed, which may be nil to
// stand for a deployment that never wired one.
func eventRoutes(t *testing.T, feed *eventtap.Feed) *httptest.Server {
	t.Helper()
	r := chi.NewRouter()
	handler.New(nil, logging.NewRingBuffer(8)).WithEventFeed(feed).RegisterData(r)
	srv := httptest.NewServer(r)
	t.Cleanup(srv.Close)
	return srv
}

// getEventRoute GETs path over real HTTP and returns the status and raw body,
// so a caller can assert on a failure response the JSON helper would reject.
// The deadline is what bounds the call when the route answers with an SSE
// stream instead of failing: the status is the verdict, the partial body is
// only there to name what came back.
func getEventRoute(t *testing.T, srv *httptest.Server, path string) (int, string) {
	t.Helper()
	client := *srv.Client()
	client.Timeout = streamWait
	resp, err := client.Get(srv.URL + path)
	if err != nil || resp == nil {
		t.Fatalf("GET %s: %v", path, err)
		return 0, ""
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(body)
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
