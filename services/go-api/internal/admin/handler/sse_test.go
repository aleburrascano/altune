package handler

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/observe/eventtap"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/httputil/httputiltest"
	"altune/go-api/internal/shared/logging"
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// TestAdminStream_StalledClientReleasesItsSlot reproduces #2004: a subscriber
// that stops reading used to block the handler in Write forever, so its slot
// was never released and every further stream was refused until a restart.
func TestAdminStream_StalledClientReleasesItsSlot(t *testing.T) {
	const idle = 200 * time.Millisecond
	quietLogs(t)
	withStreamBounds(t, idle, 5*idle)
	tap, feed := startEventFeed(t)
	srv := httputiltest.NewServer(t, eventStreamRouter(feed))
	url := srv.URL + "/events/stream"

	for i := 0; i < eventtap.MaxSubscribers; i++ {
		openStalledStream(t, srv)
	}
	if status := probeStream(t, srv, url); status != http.StatusTooManyRequests {
		t.Fatalf("status with every slot taken = %d, want 429", status)
	}

	stallClients(t, tap)
	if !slotReleasedWithin(t, srv, url, 20*idle) {
		t.Fatalf("no subscriber slot came back within %s: a stalled client still holds it", 20*idle)
	}
}

// TestAdminStream_IdleStreamEmitsKeepalive proves a stream with no events to
// send still writes, which is both what keeps proxies from dropping it and what
// makes the idle write deadline fire on a stalled client.
func TestAdminStream_IdleStreamEmitsKeepalive(t *testing.T) {
	const heartbeat = 150 * time.Millisecond
	quietLogs(t)
	withStreamBounds(t, 10*time.Second, heartbeat)
	_, feed := startEventFeed(t)
	srv := httputiltest.NewServer(t, eventStreamRouter(feed))

	conn, br := httputiltest.Get(t, srv, "/events/stream", nil)
	resp := httputiltest.ReadResponse(t, conn, br)
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	if err := conn.SetReadDeadline(time.Now().Add(6 * heartbeat)); err != nil {
		t.Fatalf("set read deadline: %v", err)
	}
	body := bufio.NewReader(resp.Body)
	for seen := 0; seen < 2; {
		line, err := body.ReadString('\n')
		if err != nil {
			t.Fatalf("saw %d keepalives in %s, want 2: %v", seen, 6*heartbeat, err)
		}
		if strings.TrimRight(line, "\r\n") == ": keepalive" {
			seen++
		}
	}
}

// TestAdminStream_EndsOnAWriteFailure covers the case the idle deadline cannot:
// a connection that rejects writes without ever cancelling the request. The
// stream must give up instead of spinning on a client it can no longer reach.
func TestAdminStream_EndsOnAWriteFailure(t *testing.T) {
	ch := make(chan eventtap.TapEvent, 1)
	ch <- eventtap.TapEvent{Type: "any"}
	w := &failingWriter{header: make(http.Header)}

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		streamSSE(w, httptest.NewRequest(http.MethodGet, "/events/stream", nil), (<-chan eventtap.TapEvent)(ch))
	}()

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("stream held its slot after every write failed")
	}
}

// failingWriter is a streaming ResponseWriter whose connection is gone: it can
// flush, but every write fails, and nothing cancels the request.
type failingWriter struct{ header http.Header }

func (w *failingWriter) Header() http.Header { return w.header }
func (w *failingWriter) WriteHeader(int)     {}
func (w *failingWriter) Flush()              {}
func (*failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("connection reset")
}

// TestAdminStreamFailures_CarryStableCode pins the two stream failures that
// answered with an uncoded 500 (#2004): callers branch on the code, and a
// stream that cannot be joined is a retryable 503, not a server fault.
func TestAdminStreamFailures_CarryStableCode(t *testing.T) {
	cases := []struct {
		name       string
		serve      func(w http.ResponseWriter, r *http.Request)
		wantStatus int
		wantCode   string
	}{
		{
			name: "subscribe failed",
			serve: func(w http.ResponseWriter, r *http.Request) {
				rejectSubscription(w, r, "logs", errors.New("ring closed"), logging.ErrTooManySubscribers)
			},
			wantStatus: http.StatusServiceUnavailable,
			wantCode:   "admin.stream_unavailable",
		},
		{
			name: "streaming unsupported",
			serve: func(w http.ResponseWriter, r *http.Request) {
				streamSSE(w, r, make(chan eventtap.TapEvent))
			},
			wantStatus: http.StatusInternalServerError,
			wantCode:   "admin.streaming_unsupported",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := &unflushableWriter{header: make(http.Header)}
			tc.serve(w, httptest.NewRequest(http.MethodGet, "/events/stream", nil))

			if w.status != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %s)", w.status, tc.wantStatus, w.body.String())
			}
			var resp struct {
				Detail string `json:"detail"`
				Code   string `json:"code"`
			}
			if err := json.Unmarshal(w.body.Bytes(), &resp); err != nil {
				t.Fatalf("decode body %q: %v", w.body.String(), err)
			}
			if resp.Code != tc.wantCode {
				t.Errorf("code = %q, want %q", resp.Code, tc.wantCode)
			}
			if resp.Detail == "" {
				t.Errorf("error response has empty detail: %s", w.body.String())
			}
		})
	}
}

// unflushableWriter is a ResponseWriter that cannot stream, the shape
// streamSSE must refuse rather than open a stream it can never flush.
type unflushableWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *unflushableWriter) Header() http.Header         { return w.header }
func (w *unflushableWriter) WriteHeader(status int)      { w.status = status }
func (w *unflushableWriter) Write(b []byte) (int, error) { return w.body.Write(b) }

// withStreamBounds compresses the stream's idle deadline and heartbeat for the
// length of the test. The restore is registered before the server, so cleanup
// order puts it after every handler goroutine has returned.
func withStreamBounds(t *testing.T, idle, heartbeat time.Duration) {
	t.Helper()
	prevIdle, prevHeartbeat := streamWriteIdle, streamHeartbeat
	streamWriteIdle, streamHeartbeat = idle, heartbeat
	t.Cleanup(func() { streamWriteIdle, streamHeartbeat = prevIdle, prevHeartbeat })
}

// quietLogs silences the per-request info lines the stall loop would otherwise
// print thousands of.
func quietLogs(t *testing.T) {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	logging.Setup("error", false)
}

func startEventFeed(t *testing.T) (*eventtap.Tap, *eventtap.Feed) {
	t.Helper()
	tap := eventtap.New(events.NewInProcessBus())
	feed := eventtap.NewFeed()
	ctx, stopFeed := context.WithCancel(context.Background())
	feed.Start(ctx, tap)
	t.Cleanup(func() {
		stopFeed()
		feed.Shutdown(context.Background())
	})
	return tap, feed
}

// eventStreamRouter mounts the admin routes behind the request logger, the
// production wrapper the per-write deadline has to reach the connection through.
func eventStreamRouter(feed *eventtap.Feed) *chi.Mux {
	r := chi.NewRouter()
	r.Use(httputil.RequestLogger)
	New(nil, nil).WithEventFeed(feed).RegisterData(r)
	return r
}

// openStalledStream opens one stream and takes only its response head, so the
// subscriber is provably registered before it stops reading — the suspended tab
// the ticket is about.
func openStalledStream(t *testing.T, srv *httptest.Server) {
	t.Helper()
	conn, br := httputiltest.Get(t, srv, "/events/stream", nil)
	resp := httputiltest.ReadResponse(t, conn, br)
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stalled client status = %d, want 200", resp.StatusCode)
	}
}

// stallClients publishes more bytes than the connections' send buffers hold, so
// every subscriber that is not reading leaves its handler blocked in Write. The
// track_id is what a tapped event carries into the frame, so it is what makes the
// frames large.
func stallClients(t *testing.T, tap *eventtap.Tap) {
	t.Helper()
	user := shared.NewUserId(uuid.New())
	payload := map[string]any{"track_id": strings.Repeat("x", 16<<10)}
	for i := 0; i < 4*eventtap.MaxSubscribers; i++ {
		tap.Publish(context.Background(), user, "stall.filler", payload)
	}
}

// slotReleasedWithin polls until the stream admits a new subscriber, which only
// happens once a stalled one's handler has given up and unsubscribed.
func slotReleasedWithin(t *testing.T, srv *httptest.Server, url string, limit time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if probeStream(t, srv, url) == http.StatusOK {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// probeStream opens and immediately closes one stream, returning its status.
func probeStream(t *testing.T, srv *httptest.Server, url string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		t.Fatalf("probe %s: %v", url, err)
	}
	_ = resp.Body.Close()
	return resp.StatusCode
}

func TestStreamSSEEndsAtMaxLifetime(t *testing.T) {
	prev := streamMaxLifetime
	streamMaxLifetime = 50 * time.Millisecond
	t.Cleanup(func() { streamMaxLifetime = prev })

	ch := make(chan string)
	done := make(chan struct{})
	go func() {
		defer close(done)
		streamSSE(httptest.NewRecorder(), httptest.NewRequest("GET", "/logs/stream", nil), ch)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream outlived its max lifetime")
	}
}

var adminStreamPaths = []string{"/logs/stream", "/events/stream"}

func TestAdminStream_EndsWhenShutdownBegins(t *testing.T) {
	for _, path := range adminStreamPaths {
		t.Run(path, func(t *testing.T) {
			shutdown := make(chan struct{})
			returned, _ := serveStream(t, shutdown, path)

			select {
			case <-returned:
				t.Fatal("stream ended before shutdown began")
			case <-time.After(100 * time.Millisecond):
			}

			close(shutdown)
			select {
			case <-returned:
			case <-time.After(2 * time.Second):
				t.Fatal("stream held its handler open after shutdown began")
			}
		})
	}
}

func TestAdminStream_OpenedAfterShutdownEndsPromptly(t *testing.T) {
	for _, path := range adminStreamPaths {
		t.Run(path, func(t *testing.T) {
			shutdown := make(chan struct{})
			close(shutdown)

			returned, _ := serveStream(t, shutdown, path)
			select {
			case <-returned:
			case <-time.After(2 * time.Second):
				t.Fatal("stream opened during shutdown stayed open")
			}
		})
	}
}

func TestAdminStream_WithoutShutdownRunsUntilTheRequestEnds(t *testing.T) {
	for _, path := range adminStreamPaths {
		t.Run(path, func(t *testing.T) {
			returned, endRequest := serveStream(t, nil, path)

			select {
			case <-returned:
				t.Fatal("stream with no shutdown channel ended on its own")
			case <-time.After(100 * time.Millisecond):
			}

			endRequest()
			select {
			case <-returned:
			case <-time.After(2 * time.Second):
				t.Fatal("stream outlived its request")
			}
		})
	}
}

func serveStream(t *testing.T, shutdown <-chan struct{}, path string) (<-chan struct{}, context.CancelFunc) {
	t.Helper()
	_, feed := startEventFeed(t)
	r := chi.NewRouter()
	New(nil, logging.NewRingBuffer(8)).WithEventFeed(feed).WithShutdown(shutdown).RegisterData(r)
	reqCtx, endRequest := context.WithCancel(context.Background())
	req := httptest.NewRequestWithContext(reqCtx, http.MethodGet, path, nil)

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}()
	t.Cleanup(func() {
		endRequest()
		<-returned
	})
	return returned, endRequest
}

func TestStreamSSEEndsAtTokenExpiry(t *testing.T) {
	req := httptest.NewRequest("GET", "/logs/stream", nil)
	req = req.WithContext(auth.ContextWithTokenExpiry(req.Context(), time.Now().Add(50*time.Millisecond)))

	ch := make(chan string)
	done := make(chan struct{})
	go func() {
		defer close(done)
		streamSSE(httptest.NewRecorder(), req, ch)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("admin stream outlived the token that authenticated it")
	}
}
