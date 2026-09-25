package handler_test

import (
	"altune/go-api/internal/admin/eventtap"
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

func TestEventRoutes_ReportADeadFeed(t *testing.T) {
	deadFeeds := []struct {
		name string
		feed *eventtap.Feed
	}{
		{"feed not wired", nil},
		{"feed could not subscribe", unsubscribedFeed(t)},
	}

	for _, dead := range deadFeeds {
		for _, path := range []string{"/events/stream"} {
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
