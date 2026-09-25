package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httputil/httputiltest"
	"altune/go-api/internal/shared/logging"
	"bufio"
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestRouter_PanickingHandlerLogsRequestCompleteAs500Error(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("error", false)

	a := &App{cfg: &config.Config{Env: "test"}}
	r := a.newRouter(apiWriteTimeout)
	r.Get("/v1/boom", func(http.ResponseWriter, *http.Request) {
		panic("handler exploded")
	})
	rec := httptest.NewRecorder()

	r.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/boom", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("response status = %d, want 500", rec.Code)
	}
	var completes []logging.CapturedRecord
	for _, captured := range ring.Snapshot() {
		if captured.Message == "request.complete" {
			completes = append(completes, captured)
		}
	}
	if len(completes) != 1 {
		t.Fatalf("request.complete records = %d, want 1", len(completes))
	}
	if got := completes[0].Attrs["status"]; got != "500" {
		t.Errorf("request.complete status = %s, want 500", got)
	}
	if got := completes[0].Level; got != slog.LevelError.String() {
		t.Errorf("request.complete level = %s, want %s", got, slog.LevelError)
	}
}

const routeWriteDeadline = 150 * time.Millisecond

// newDeadlineRouter builds the production root router (newRouter) with a short
// write deadline and mounts a large non-SSE response next to /v1/events.
// bulkErrs receives the error that ended the bulk handler's writes.
func newDeadlineRouter(uid shared.UserId, sse *sseHandler, bulkErrs chan<- error) *chi.Mux {
	a := &App{cfg: &config.Config{Env: "test"}}
	r := a.newRouter(routeWriteDeadline)
	r.Get("/v1/bulk", func(w http.ResponseWriter, _ *http.Request) {
		chunk := bytes.Repeat([]byte("x"), 16<<10)
		giveUp := time.Now().Add(10 * time.Second)
		for time.Now().Before(giveUp) {
			if _, err := w.Write(chunk); err != nil {
				bulkErrs <- err
				return
			}
		}
		bulkErrs <- nil
	})
	r.Handle("/v1/events", http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		sse.ServeHTTP(w, req.WithContext(auth.ContextWithUserID(req.Context(), uid)))
	}))
	return r
}

// TestRouter_WriteDeadlineCutsOffSlowReaderOnNonSSERoute is the regression
// guard for #1018: a client that stops reading a non-SSE response must not hold
// the handler goroutine blocked in Write past the route write deadline.
func TestRouter_WriteDeadlineCutsOffSlowReaderOnNonSSERoute(t *testing.T) {
	bulkErrs := make(chan error, 1)
	sse := newSSEHandler(events.NewInProcessBus(), 0)
	srv := httputiltest.NewServer(t, newDeadlineRouter(shared.NewUserId(uuid.New()), sse, bulkErrs))

	began := time.Now()
	httputiltest.Get(t, srv, "/v1/bulk", nil) // never reads

	select {
	case err := <-bulkErrs:
		if err == nil {
			t.Fatal("bulk writes never failed: slow reader held the handler open")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("bulk handler still blocked in Write after 3s: no write deadline on non-SSE route")
	}
	if elapsed := time.Since(began); elapsed < routeWriteDeadline {
		t.Errorf("cut off after %s, before the %s deadline", elapsed, routeWriteDeadline)
	}
}

// TestRouter_SSEStreamOutlivesRouteWriteDeadline proves /v1/events keeps
// delivering heartbeats and events long after the route write deadline.
func TestRouter_SSEStreamOutlivesRouteWriteDeadline(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	sse := newSSEHandler(bus, 0)
	sse.heartbeat = routeWriteDeadline / 3
	srv := httputiltest.NewServer(t, newDeadlineRouter(uid, sse, make(chan error, 1)))

	conn, br := httputiltest.Get(t, srv, "/v1/events", nil)
	resp := httputiltest.ReadResponse(t, conn, br)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body := bufio.NewReader(resp.Body)

	time.Sleep(4 * routeWriteDeadline)
	bus.Publish(context.Background(), uid, "late.event", map[string]any{"k": "v"})
	readUntil(t, body, func(l string) bool { return strings.HasPrefix(l, "event: late.event") })
}

// TestRouter_SSEPerFrameDeadlineReachesConnection proves the stream's own
// per-frame write deadline takes effect through the router's middleware
// wrappers: a stalled SSE client is cut off instead of blocking forever.
func TestRouter_SSEPerFrameDeadlineReachesConnection(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	slog.SetDefault(slog.New(slog.DiscardHandler)) // the stalled subscriber's drops are expected noise

	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	sse := newSSEHandler(bus, 0)
	sse.writeTimeout = routeWriteDeadline
	sse.heartbeat = time.Hour
	done := make(chan struct{})
	router := newDeadlineRouter(uid, sse, make(chan error, 1))
	srv := httputiltest.NewServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		router.ServeHTTP(w, r)
	}))

	httputiltest.Get(t, srv, "/v1/events", nil) // never reads

	payload := map[string]any{"pad": strings.Repeat("x", 16<<10)}
	giveUp := time.After(5 * time.Second)
	for {
		select {
		case <-done:
			return
		case <-giveUp:
			t.Fatal("SSE handler still blocked on a stalled client after 5s: per-frame deadline not applied")
		default:
			bus.Publish(context.Background(), uid, "bulk", payload)
			time.Sleep(5 * time.Millisecond)
		}
	}
}
