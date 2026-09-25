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
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

const streamWait = 5 * time.Second

var observeStreamPaths = []string{"/events/stream", "/logs/stream"}

type streamFixture struct {
	tap  *eventtap.Tap
	ring *logging.RingBuffer
	deps Deps
}

func newStreamFixture(t *testing.T) streamFixture {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	ring := logging.Setup("error", false)
	tap := eventtap.New(events.NewInProcessBus())
	feed := eventtap.NewFeed()
	ctx, stopFeed := context.WithCancel(context.Background())
	feed.Start(ctx, tap)
	t.Cleanup(func() {
		stopFeed()
		feed.Shutdown(context.Background())
	})
	return streamFixture{tap: tap, ring: ring, deps: Deps{Events: feed, Logs: ring}}
}

func streamRouter(deps Deps, middlewares ...func(http.Handler) http.Handler) *chi.Mux {
	r := chi.NewRouter()
	r.Use(middlewares...)
	New(deps).Register(r)
	return r
}

type sseConn struct {
	cancel context.CancelFunc
	header http.Header
	lines  chan string
}

func (c *sseConn) close() { c.cancel() }

func (c *sseConn) awaitData(t *testing.T, want string) string {
	t.Helper()
	deadline := time.After(streamWait)
	for {
		select {
		case line, open := <-c.lines:
			if !open {
				t.Fatalf("stream closed before a frame containing %q arrived", want)
			}
			if strings.HasPrefix(line, "data: ") && strings.Contains(line, want) {
				return strings.TrimPrefix(line, "data: ")
			}
		case <-deadline:
			t.Fatalf("no data frame containing %q within %s", want, streamWait)
		}
	}
}

func dialStream(t *testing.T, srv *httptest.Server, path string) (int, *sseConn, string) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+path, nil)
	if err != nil {
		cancel()
		t.Fatalf("new request: %v", err)
	}
	resp, err := srv.Client().Do(req)
	if err != nil {
		cancel()
		t.Fatalf("dial %s: %v", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		cancel()
		return resp.StatusCode, nil, string(body)
	}
	conn := &sseConn{cancel: cancel, header: resp.Header, lines: make(chan string, 256)}
	go readLines(resp.Body, conn.lines)
	t.Cleanup(conn.close)
	return http.StatusOK, conn, ""
}

func readLines(body io.ReadCloser, lines chan<- string) {
	defer close(lines)
	defer func() { _ = body.Close() }()
	sc := bufio.NewScanner(body)
	for sc.Scan() {
		select {
		case lines <- sc.Text():
		default:
		}
	}
}

func openStream(t *testing.T, srv *httptest.Server, path string) *sseConn {
	t.Helper()
	status, conn, body := dialStream(t, srv, path)
	if status != http.StatusOK {
		t.Fatalf("GET %s: status %d, want 200 (body %s)", path, status, body)
	}
	return conn
}

func decodeFrame(t *testing.T, frame string) map[string]json.RawMessage {
	t.Helper()
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(frame), &fields); err != nil {
		t.Fatalf("decode frame %s: %v", frame, err)
	}
	return fields
}

func withCorrelation(id string) context.Context {
	return logging.WithCorrelationID(context.Background(), id)
}

func assertCode(t *testing.T, rec *httptest.ResponseRecorder, wantStatus int, wantCode string) {
	t.Helper()
	if rec.Code != wantStatus {
		t.Fatalf("status = %d, want %d (body %s)", rec.Code, wantStatus, rec.Body.String())
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %s: %v", rec.Body.String(), err)
	}
	if body.Code != wantCode {
		t.Errorf("code = %q, want %q", body.Code, wantCode)
	}
}

func serveOnce(deps Deps, path string) *httptest.ResponseRecorder {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	rec := httptest.NewRecorder()
	streamRouter(deps).ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil))
	return rec
}

func TestStreamEvents_ForwardsEachPublishedEventAsADataFrame(t *testing.T) {
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps))
	t.Cleanup(srv.Close)
	conn := openStream(t, srv, "/events/stream")

	marker := "event-" + uuid.NewString()
	fx.tap.Publish(context.Background(), shared.NewUserId(uuid.New()), marker, map[string]any{"track_id": "trk-1"})

	fields := decodeFrame(t, conn.awaitData(t, marker))
	if string(fields["type"]) != `"`+marker+`"` || string(fields["subject"]) != `"trk-1"` {
		t.Errorf("frame = %v, want type %s and subject trk-1", fields, marker)
	}
}

func TestStreamLogs_ForwardsEachRingRecordAsADataFrame(t *testing.T) {
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps))
	t.Cleanup(srv.Close)
	conn := openStream(t, srv, "/logs/stream")

	marker := "log-" + uuid.NewString()
	slog.Warn(marker, slog.String("stage", "resolve"))

	fields := decodeFrame(t, conn.awaitData(t, marker))
	if string(fields["level"]) != `"WARN"` || !strings.Contains(string(fields["attrs"]), `"stage":"resolve"`) {
		t.Errorf("frame = %v, want level WARN and attr stage=resolve", fields)
	}
}

func TestStreamEvents_FrameCarriesTheUserDigestNeverTheRawID(t *testing.T) {
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps))
	t.Cleanup(srv.Close)
	conn := openStream(t, srv, "/events/stream")
	user := shared.NewUserId(uuid.New())

	marker := "private-" + uuid.NewString()
	fx.tap.Publish(context.Background(), user, marker, map[string]any{"query": "my private query"})

	frame := conn.awaitData(t, marker)
	if strings.Contains(frame, user.String()) || strings.Contains(frame, "my private query") {
		t.Fatalf("frame leaks the raw user or search text: %s", frame)
	}
	sum := sha256.Sum256([]byte(user.String()))
	if want := `"user":"` + hex.EncodeToString(sum[:4]) + `"`; !strings.Contains(frame, want) {
		t.Errorf("frame %s lacks the user digest %s", frame, want)
	}
}

func TestStreamEvents_FrameCarriesTheOriginatingCorrID(t *testing.T) {
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps))
	t.Cleanup(srv.Close)
	conn := openStream(t, srv, "/events/stream")

	marker := "corr-" + uuid.NewString()
	fx.tap.Publish(withCorrelation("evt-corr-1"), shared.NewUserId(uuid.New()), marker, nil)

	if frame := conn.awaitData(t, marker); !strings.Contains(frame, `"corr_id":"evt-corr-1"`) {
		t.Errorf("frame %s lacks the originating corr_id", frame)
	}
}

func TestStreamLogs_FrameIsTheRedactedRecordWithItsCorrID(t *testing.T) {
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps))
	t.Cleanup(srv.Close)
	conn := openStream(t, srv, "/logs/stream")

	marker := "redacted-" + uuid.NewString()
	slog.InfoContext(withCorrelation("log-corr-1"), marker,
		slog.String("password", "hunter2"),
		slog.String("query", "my private query"),
	)

	frame := conn.awaitData(t, marker)
	if strings.Contains(frame, "hunter2") || strings.Contains(frame, "my private query") {
		t.Fatalf("log frame carries a value the redactor drops: %s", frame)
	}
	if !strings.Contains(frame, `"corr_id":"log-corr-1"`) {
		t.Errorf("log frame %s lacks the originating corr_id", frame)
	}
}

func TestStreamEvents_AbsentOrIdleFeedIsUnavailable(t *testing.T) {
	cases := []struct {
		name string
		feed *eventtap.Feed
	}{
		{"no feed wired", nil},
		{"feed never started", eventtap.NewFeed()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := serveOnce(Deps{Events: tc.feed}, "/events/stream")

			assertCode(t, rec, http.StatusServiceUnavailable, "observe.event_feed_unavailable")
		})
	}
}

func TestStreamLogs_AbsentRingIsUnavailable(t *testing.T) {
	rec := serveOnce(Deps{}, "/logs/stream")

	assertCode(t, rec, http.StatusServiceUnavailable, "observe.stream_unavailable")
}

func TestStreams_SendEventStreamNoCacheAndNosniffHeaders(t *testing.T) {
	fx := newStreamFixture(t)
	for _, path := range observeStreamPaths {
		t.Run(path, func(t *testing.T) {
			rec := httptest.NewRecorder()
			ctx, cancel := context.WithCancel(context.Background())
			cancel()

			streamRouter(fx.deps, NoStoreAndNosniff).ServeHTTP(rec, httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil))

			want := map[string]string{"Content-Type": "text/event-stream", "Cache-Control": "no-cache", "X-Content-Type-Options": "nosniff"}
			for header, value := range want {
				if got := rec.Header().Get(header); got != value {
					t.Errorf("%s = %q, want %q", header, got, value)
				}
			}
			if !strings.HasPrefix(rec.Body.String(), keepaliveFrame) {
				t.Errorf("body %q does not open with the keepalive frame", rec.Body.String())
			}
		})
	}
}

func TestStreams_OpenWritesOneObserveReadAuditLine(t *testing.T) {
	fx := newStreamFixture(t)
	for _, path := range observeStreamPaths {
		t.Run(path, func(t *testing.T) {
			var logs bytes.Buffer
			slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))
			principal := shared.NewUserId(uuid.New())
			ctx, cancel := context.WithCancel(auth.ContextWithUserID(withCorrelation("audit-corr-1"), principal))
			cancel()

			streamRouter(fx.deps).ServeHTTP(httptest.NewRecorder(), httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil))

			if n := strings.Count(logs.String(), `"msg":"observe.read"`); n != 1 {
				t.Fatalf("observe.read lines = %d, want 1; logs %s", n, logs.String())
			}
			for _, want := range []string{`"actor":"` + principal.String() + `"`, `"method":"GET"`, `"path":"` + path + `"`, `"corr_id":"audit-corr-1"`} {
				if !strings.Contains(logs.String(), want) {
					t.Errorf("audit line %s lacks %s", logs.String(), want)
				}
			}
		})
	}
}

func TestStreams_AuditNamesAnUnknownActorWhenNoSubjectReachedIt(t *testing.T) {
	var logs bytes.Buffer
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	slog.SetDefault(slog.New(slog.NewJSONHandler(&logs, nil)))

	serveOnce(Deps{}, "/logs/stream")

	if !strings.Contains(logs.String(), `"actor":"unknown"`) {
		t.Errorf("audit line %s does not name the actor unknown", logs.String())
	}
}

func serveStream(t *testing.T, deps Deps, req *http.Request) <-chan struct{} {
	t.Helper()
	ctx, endRequest := context.WithCancel(req.Context())
	returned := make(chan struct{})
	go func() {
		defer close(returned)
		streamRouter(deps).ServeHTTP(httptest.NewRecorder(), req.WithContext(ctx))
	}()
	t.Cleanup(func() {
		endRequest()
		<-returned
	})
	return returned
}

func assertEndsWithin(t *testing.T, returned <-chan struct{}, limit time.Duration, why string) {
	t.Helper()
	select {
	case <-returned:
	case <-time.After(limit):
		t.Fatal(why)
	}
}

func assertStillOpen(t *testing.T, returned <-chan struct{}, why string) {
	t.Helper()
	select {
	case <-returned:
		t.Fatal(why)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestStreams_EndAtMaxLifetime(t *testing.T) {
	prev := streamMaxLifetime
	streamMaxLifetime = 50 * time.Millisecond
	t.Cleanup(func() { streamMaxLifetime = prev })
	fx := newStreamFixture(t)
	for _, path := range observeStreamPaths {
		t.Run(path, func(t *testing.T) {
			returned := serveStream(t, fx.deps, httptest.NewRequest(http.MethodGet, path, nil))

			assertEndsWithin(t, returned, 2*time.Second, "stream outlived its max lifetime")
		})
	}
}

func TestStreams_DefaultBoundsMatchTheAdminStreams(t *testing.T) {
	bounds := []struct {
		name string
		got  time.Duration
		want time.Duration
	}{
		{"max lifetime", streamMaxLifetime, 15 * time.Minute},
		{"write idle", streamWriteIdle, 10 * time.Second},
		{"heartbeat", streamHeartbeat, 25 * time.Second},
	}
	for _, bound := range bounds {
		if bound.got != bound.want {
			t.Errorf("%s = %s, want %s", bound.name, bound.got, bound.want)
		}
	}
}

func TestStreams_KeepForwardingAfterTheFirstFrame(t *testing.T) {
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps))
	t.Cleanup(srv.Close)
	conn := openStream(t, srv, "/events/stream")
	user := shared.NewUserId(uuid.New())

	first, second := "first-"+uuid.NewString(), "second-"+uuid.NewString()
	fx.tap.Publish(context.Background(), user, first, nil)
	conn.awaitData(t, first)
	fx.tap.Publish(context.Background(), user, second, nil)

	conn.awaitData(t, second)
}

func TestStreams_EndAtTokenExpiry(t *testing.T) {
	fx := newStreamFixture(t)
	for _, path := range observeStreamPaths {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			req = req.WithContext(auth.ContextWithTokenExpiry(req.Context(), time.Now().Add(50*time.Millisecond)))

			returned := serveStream(t, fx.deps, req)

			assertEndsWithin(t, returned, 2*time.Second, "stream outlived the token that authenticated it")
		})
	}
}

func TestStreams_EndWhenShutdownBeginsWhileIdle(t *testing.T) {
	fx := newStreamFixture(t)
	for _, path := range observeStreamPaths {
		t.Run(path, func(t *testing.T) {
			shutdown := make(chan struct{})
			deps := fx.deps
			deps.Shutdown = shutdown
			returned := serveStream(t, deps, httptest.NewRequest(http.MethodGet, path, nil))
			assertStillOpen(t, returned, "stream ended before shutdown began")

			close(shutdown)

			assertEndsWithin(t, returned, 2*time.Second, "idle stream held its handler open after shutdown began")
		})
	}
}

func TestStreams_OpenedAfterShutdownEndPromptly(t *testing.T) {
	fx := newStreamFixture(t)
	for _, path := range observeStreamPaths {
		t.Run(path, func(t *testing.T) {
			shutdown := make(chan struct{})
			close(shutdown)
			deps := fx.deps
			deps.Shutdown = shutdown

			returned := serveStream(t, deps, httptest.NewRequest(http.MethodGet, path, nil))

			assertEndsWithin(t, returned, 2*time.Second, "stream opened during shutdown stayed open")
		})
	}
}

func TestStreams_WithoutShutdownRunUntilTheRequestEnds(t *testing.T) {
	fx := newStreamFixture(t)
	for _, path := range observeStreamPaths {
		t.Run(path, func(t *testing.T) {
			ctx, endRequest := context.WithCancel(context.Background())
			returned := serveStream(t, fx.deps, httptest.NewRequestWithContext(ctx, http.MethodGet, path, nil))
			assertStillOpen(t, returned, "stream with no shutdown channel ended on its own")

			endRequest()

			assertEndsWithin(t, returned, 2*time.Second, "stream outlived its request")
		})
	}
}

func fillSubscriberSlots(t *testing.T, fx streamFixture, path string) {
	t.Helper()
	subscribers := map[string]struct {
		limit     int
		subscribe func() (func(), error)
	}{
		"/events/stream": {eventtap.MaxSubscribers, func() (func(), error) {
			_, cancel, err := fx.deps.Events.Subscribe()
			return cancel, err
		}},
		"/logs/stream": {logging.MaxSubscribers, func() (func(), error) {
			_, cancel, err := fx.deps.Logs.Subscribe()
			return cancel, err
		}},
	}
	limit, subscribe := subscribers[path].limit, subscribers[path].subscribe
	for i := 0; i < limit; i++ {
		cancel, err := subscribe()
		if err != nil {
			t.Fatalf("subscriber %d of %d: %v", i+1, limit, err)
		}
		t.Cleanup(cancel)
	}
}

func TestStreams_SubscriberPastTheCeilingGets429(t *testing.T) {
	for _, path := range observeStreamPaths {
		t.Run(path, func(t *testing.T) {
			fx := newStreamFixture(t)
			fillSubscriberSlots(t, fx, path)

			rec := serveOnce(fx.deps, path)

			assertCode(t, rec, http.StatusTooManyRequests, "observe.stream_subscriber_limit")
		})
	}
}

func TestStreams_SixteenthSubscriberIsStillAdmitted(t *testing.T) {
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps))
	t.Cleanup(srv.Close)
	for _, path := range observeStreamPaths {
		t.Run(path, func(t *testing.T) {
			for i := 0; i < eventtap.MaxSubscribers-1; i++ {
				openStream(t, srv, path)
			}

			status, _, body := dialStream(t, srv, path)

			if status != http.StatusOK {
				t.Fatalf("subscriber %d: status %d, want 200 (body %s)", eventtap.MaxSubscribers, status, body)
			}
		})
	}
}

func TestStreams_SubscribeFailureOtherThanTheCeilingIs503(t *testing.T) {
	rec := httptest.NewRecorder()

	rejectSubscription(rec, httptest.NewRequest(http.MethodGet, "/logs/stream", nil), "logs", errors.New("ring closed"), logging.ErrTooManySubscribers)

	assertCode(t, rec, http.StatusServiceUnavailable, "observe.stream_unavailable")
}

type unflushableWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (w *unflushableWriter) Header() http.Header         { return w.header }
func (w *unflushableWriter) WriteHeader(status int)      { w.status = status }
func (w *unflushableWriter) Write(b []byte) (int, error) { return w.body.Write(b) }

func TestStreams_UnflushableWriterIsRefusedWithACodedError(t *testing.T) {
	w := &unflushableWriter{header: make(http.Header)}

	streamSSE(w, httptest.NewRequest(http.MethodGet, "/events/stream", nil), make(chan eventtap.TapEvent))

	if w.status != http.StatusInternalServerError || !strings.Contains(w.body.String(), `"code":"observe.streaming_unsupported"`) {
		t.Errorf("status %d body %s, want 500 observe.streaming_unsupported", w.status, w.body.String())
	}
}

type failingWriter struct{ header http.Header }

func (w *failingWriter) Header() http.Header { return w.header }
func (w *failingWriter) WriteHeader(int)     {}
func (w *failingWriter) Flush()              {}
func (*failingWriter) Write([]byte) (int, error) {
	return 0, errors.New("connection reset")
}

func TestStreams_EndOnAWriteFailure(t *testing.T) {
	ch := make(chan eventtap.TapEvent, 1)
	ch <- eventtap.TapEvent{Type: "any"}
	returned := make(chan struct{})

	go func() {
		defer close(returned)
		streamSSE(&failingWriter{header: make(http.Header)}, httptest.NewRequest(http.MethodGet, "/events/stream", nil), (<-chan eventtap.TapEvent)(ch))
	}()

	assertEndsWithin(t, returned, 2*time.Second, "stream held its slot after every write failed")
}

func withStreamBounds(t *testing.T, idle, heartbeat time.Duration) {
	t.Helper()
	prevIdle, prevHeartbeat := streamWriteIdle, streamHeartbeat
	streamWriteIdle, streamHeartbeat = idle, heartbeat
	t.Cleanup(func() { streamWriteIdle, streamHeartbeat = prevIdle, prevHeartbeat })
}

func TestStreams_IdleStreamEmitsKeepalives(t *testing.T) {
	const heartbeat = 150 * time.Millisecond
	withStreamBounds(t, 10*time.Second, heartbeat)
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps))
	t.Cleanup(srv.Close)
	conn := openStream(t, srv, "/events/stream")

	deadline := time.After(6 * heartbeat)
	for seen := 0; seen < 3; {
		select {
		case line := <-conn.lines:
			if line == ": keepalive" {
				seen++
			}
		case <-deadline:
			t.Fatalf("saw %d keepalives in %s, want the opening one plus 2", seen, 6*heartbeat)
		}
	}
}

func TestStreams_OutliveTheRouteWriteDeadline(t *testing.T) {
	const routeDeadline = 150 * time.Millisecond
	fx := newStreamFixture(t)
	srv := httptest.NewServer(streamRouter(fx.deps, httputil.WriteDeadline(routeDeadline), httputil.RequestLogger))
	t.Cleanup(srv.Close)
	emit := map[string]func(string){
		"/events/stream": func(marker string) { fx.tap.Publish(context.Background(), shared.NewUserId(uuid.New()), marker, nil) },
		"/logs/stream":   func(marker string) { slog.Error(marker) },
	}
	for _, path := range observeStreamPaths {
		t.Run(path, func(t *testing.T) {
			conn := openStream(t, srv, path)
			time.Sleep(3 * routeDeadline)

			marker := "late-" + uuid.NewString()
			emit[path](marker)

			conn.awaitData(t, marker)
		})
	}
}

func openStalledStream(t *testing.T, srv *httptest.Server) {
	t.Helper()
	conn, br := httputiltest.Get(t, srv, "/events/stream", nil)
	resp := httputiltest.ReadResponse(t, conn, br)
	t.Cleanup(func() { _ = resp.Body.Close() })
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stalled client status = %d, want 200", resp.StatusCode)
	}
}

func stallClients(tap *eventtap.Tap) {
	user := shared.NewUserId(uuid.New())
	payload := map[string]any{"track_id": strings.Repeat("x", 16<<10)}
	for i := 0; i < 4*eventtap.MaxSubscribers; i++ {
		tap.Publish(context.Background(), user, "stall.filler", payload)
	}
}

func probeStream(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	status, conn, _ := dialStream(t, srv, "/events/stream")
	if conn != nil {
		conn.close()
	}
	return status
}

func slotReleasedWithin(t *testing.T, srv *httptest.Server, limit time.Duration) bool {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if probeStream(t, srv) == http.StatusOK {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

func TestStreams_StalledClientReleasesItsSlotAtTheWriteDeadline(t *testing.T) {
	const idle = 200 * time.Millisecond
	withStreamBounds(t, idle, 5*idle)
	fx := newStreamFixture(t)
	srv := httputiltest.NewServer(t, streamRouter(fx.deps, httputil.RequestLogger))
	for i := 0; i < eventtap.MaxSubscribers; i++ {
		openStalledStream(t, srv)
	}
	if status := probeStream(t, srv); status != http.StatusTooManyRequests {
		t.Fatalf("status with every slot taken = %d, want 429", status)
	}

	stallClients(fx.tap)

	if !slotReleasedWithin(t, srv, 20*idle) {
		t.Fatalf("no subscriber slot came back within %s: a stalled client still holds it", 20*idle)
	}
}
