package handler_test

import (
	"altune/go-api/internal/admin/handler"
	"altune/go-api/internal/observe/eventtap"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// TestAdminStreams_OutliveRouteWriteDeadline guards #1018: the admin live tails
// run behind the router-wide write deadline, and must keep delivering frames
// long after it has elapsed.
func TestAdminStreams_OutliveRouteWriteDeadline(t *testing.T) {
	const routeDeadline = 150 * time.Millisecond

	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("error", false)

	bus := events.NewInProcessBus()
	tap := eventtap.New(bus)
	feed := eventtap.NewFeed()
	feedCtx, stopFeed := context.WithCancel(context.Background())
	defer func() {
		stopFeed()
		feed.Shutdown(context.Background())
	}()
	feed.Start(feedCtx, tap)

	r := chi.NewRouter()
	r.Use(httputil.WriteDeadline(routeDeadline))
	r.Use(httputil.RequestLogger)
	handler.New(nil, ring).WithEventFeed(feed).RegisterData(r)
	srv := httptest.NewServer(r)
	defer srv.Close()

	user := shared.NewUserId(uuid.New())
	cases := []streamCase{
		{name: "logs", path: "/logs/stream", emit: func(marker string) { slog.Error(marker) }},
		{name: "events", path: "/events/stream", emit: func(marker string) { tap.Publish(context.Background(), user, marker, nil) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, conn, body := dialStream(t, srv.Client(), srv.URL+tc.path)
			if status != http.StatusOK {
				t.Fatalf("status = %d, want 200 (body %q)", status, body)
			}
			defer conn.close()

			time.Sleep(3 * routeDeadline)
			marker := "late-" + tc.name + "-" + uuid.NewString()
			tc.emit(marker)
			conn.awaitData(t, marker)
		})
	}
}
