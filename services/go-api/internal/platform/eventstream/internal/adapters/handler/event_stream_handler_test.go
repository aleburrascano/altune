package handler_test

import (
	handler "altune/go-api/internal/platform/eventstream/internal/adapters/handler"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"altune/go-api/internal/shared/requestuser"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestSSEServer(t *testing.T, bus *events.InProcessBus, uid ids.UserId, heartbeat time.Duration) *httptest.Server {
	t.Helper()
	h := handler.NewEventStreamHandler(bus, 0)
	h.WithHeartbeat(heartbeat)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(requestuser.ContextWithUserID(r.Context(), uid))
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readUntil(t *testing.T, r *bufio.Reader, match func(string) bool) string {
	t.Helper()
	deadline := time.After(2 * time.Second)
	for {
		line := readLineBefore(t, r, deadline)
		if match(strings.TrimRight(line, "\n")) {
			return line
		}
	}
}

type streamLine struct {
	text string
	err  error
}

func readLineBefore(t *testing.T, r *bufio.Reader, deadline <-chan time.Time) string {
	t.Helper()
	read := make(chan streamLine, 1)
	go func() {
		text, err := r.ReadString('\n')
		read <- streamLine{text: text, err: err}
	}()
	select {
	case got := <-read:
		if got.err != nil {
			t.Fatalf("reading stream: %v", got.err)
		}
		return got.text
	case <-deadline:
		t.Fatal("timed out waiting for expected line")
		return ""
	}
}

func TestSSEHandler_CaughtUpReconnectStreamsLiveEvents(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	bus.Publish(context.Background(), uid, "seed", map[string]any{"k": "v"})

	srv := newTestSSEServer(t, bus, uid, 50*time.Millisecond)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", "1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (must not 204 on caught-up reconnect)", resp.StatusCode)
	}

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return strings.HasPrefix(l, ":") })

	bus.Publish(context.Background(), uid, "live", map[string]any{"hello": "world"})
	readUntil(t, br, func(l string) bool { return l == "event: live" })
}

func TestSSEHandler_ReplayGapEmitsResync(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	bus.Publish(context.Background(), uid, "seed", map[string]any{"k": "v"})

	srv := newTestSSEServer(t, bus, uid, 50*time.Millisecond)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", "1")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return l == "event: resync" })
}

func TestSSEHandler_MarshalFailureEmitsResync(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())

	srv := newTestSSEServer(t, bus, uid, 50*time.Millisecond)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return strings.HasPrefix(l, ":") })

	bus.Publish(context.Background(), uid, "unmarshalable", map[string]any{"bad": make(chan int)})
	readUntil(t, br, func(l string) bool { return l == "event: resync" })
}

func TestSSEHandler_MalformedLastEventIDEmitsResync(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	bus.Publish(context.Background(), uid, "seed", map[string]any{"k": "v"})

	srv := newTestSSEServer(t, bus, uid, 50*time.Millisecond)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", "not-a-number")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return l == "event: resync" })
}

func TestSSEHandler_OutOfRangeLastEventIDResyncsAndStreamsLive(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	issued, err := strconv.ParseUint(lastEventIDFor(t, bus, uid), 10, 64)
	if err != nil {
		t.Fatal(err)
	}

	srv := newTestSSEServer(t, bus, uid, 50*time.Millisecond)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", strconv.FormatUint(issued+1_000_000, 10))

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return l == "event: resync" })
	readUntil(t, br, func(l string) bool { return l == ":ok" })

	bus.Publish(context.Background(), uid, "live", map[string]any{"hello": "world"})
	readUntil(t, br, func(l string) bool { return l == "event: live" })
}

func TestSSEHandler_LastEventIDAtHighWaterMarkIsCaughtUp(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	lastID := lastEventIDFor(t, bus, uid)

	srv := newTestSSEServer(t, bus, uid, time.Hour)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", lastID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	first := readUntil(t, br, func(l string) bool { return l != "" })
	if strings.TrimRight(first, "\n") != ":ok" {
		t.Fatalf("first frame line = %q, want :ok (no resync for a caught-up resume)", first)
	}
}

func waitForLog(t *testing.T, ring *logging.RingBuffer, msg string) logging.CapturedRecord {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		for _, r := range ring.Snapshot() {
			if r.Message == msg {
				return r
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q log line", msg)
	return logging.CapturedRecord{}
}

func TestSSEHandler_ConnectDisconnectLogsCarryCorrelationID(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("debug", false)

	const corrID = "corr-sse-1234"
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	h := handler.NewEventStreamHandler(bus, 0)
	h.WithHeartbeat(50 * time.Millisecond)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := logging.WithCorrelationID(requestuser.ContextWithUserID(r.Context(), uid), corrID)
		h.ServeHTTP(w, r.WithContext(ctx))
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return strings.HasPrefix(l, ":") })

	cancel()
	resp.Body.Close()

	if got := waitForLog(t, ring, "sse.connected").Attrs["corr_id"]; got != corrID {
		t.Errorf("sse.connected corr_id = %q, want %q", got, corrID)
	}
	if got := waitForLog(t, ring, "sse.disconnected").Attrs["corr_id"]; got != corrID {
		t.Errorf("sse.disconnected corr_id = %q, want %q", got, corrID)
	}
}

func resumeStream(t *testing.T, srv *httptest.Server, lastEventID string) *bufio.Reader {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", lastEventID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return bufio.NewReader(resp.Body)
}

func firstEventOf(t *testing.T, br *bufio.Reader, want string) string {
	t.Helper()
	line := readUntil(t, br, func(l string) bool { return l == "event: "+want || l == "event: resync" })
	return strings.TrimRight(line, "\n")
}

func TestSSEHandler_ResumeWithContiguousHistoryReplaysWithoutResync(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	lastID := lastEventIDFor(t, bus, uid)
	bus.Publish(context.Background(), uid, "missed", map[string]any{"k": "v"})

	br := resumeStream(t, newTestSSEServer(t, bus, uid, time.Hour), lastID)

	if got := firstEventOf(t, br, "missed"); got != "event: missed" {
		t.Fatalf("first frame on a contiguous resume = %q, want the missed event replayed", got)
	}
}

const moreEventsThanTheBusRetains = 1000

func TestSSEHandler_ResumeFromBeforeTheRetainedHistoryResyncs(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	seed, err := strconv.ParseUint(lastEventIDFor(t, bus, uid), 10, 64)
	if err != nil {
		t.Fatal(err)
	}
	for i := range moreEventsThanTheBusRetains {
		bus.Publish(context.Background(), uid, "later", map[string]any{"i": i})
	}

	br := resumeStream(t, newTestSSEServer(t, bus, uid, time.Hour), strconv.FormatUint(seed-1, 10))

	if got := firstEventOf(t, br, "later"); got != "event: resync" {
		t.Fatalf("first frame when the seed event aged out of the history = %q, want event: resync", got)
	}
}

func TestSSEHandler_ConsecutiveLiveEventsArriveWithoutResync(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	srv := newTestSSEServer(t, bus, uid, time.Hour)
	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return l == ":ok" })

	bus.Publish(context.Background(), uid, "one", map[string]any{"k": "v"})
	bus.Publish(context.Background(), uid, "two", map[string]any{"k": "v"})

	if got := firstEventOf(t, br, "two"); got != "event: two" {
		t.Fatalf("frame before the second live event = %q, want no resync between consecutive ids", got)
	}
}

type busThatHasIssuedNothing struct{ *events.InProcessBus }

func (busThatHasIssuedNothing) LatestID(ids.UserId) uint64 { return 0 }

func TestSSEHandler_FirstLiveEventFromABusThatHadIssuedNothingIsNotAGap(t *testing.T) {
	real := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	h := handler.NewEventStreamHandler(busThatHasIssuedNothing{real}, 0).WithHeartbeat(time.Hour)
	resp, err := http.Get(serveSSE(t, h, uid).URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return l == ":ok" })

	real.Publish(context.Background(), uid, "first", map[string]any{"k": "v"})

	if got := firstEventOf(t, br, "first"); got != "event: first" {
		t.Fatalf("first frame = %q, want the event without a resync when nothing was delivered before it", got)
	}
}

type busWithGapPublish struct {
	*events.InProcessBus
	uid  ids.UserId
	once sync.Once
}

func (b *busWithGapPublish) Replay(userId ids.UserId, afterID uint64) []events.Event {
	snapshot := b.InProcessBus.Replay(userId, afterID)
	b.once.Do(func() {
		b.Publish(context.Background(), b.uid, "gap", map[string]any{"in": "window"})
	})
	return snapshot
}

type busWithDupPublish struct {
	*events.InProcessBus
	uid  ids.UserId
	once sync.Once
}

func (b *busWithDupPublish) Subscribe(userId ids.UserId) (<-chan events.Event, func()) {
	ch, cancel := b.InProcessBus.Subscribe(userId)
	b.once.Do(func() {
		b.Publish(context.Background(), b.uid, "dup", map[string]any{"seen": "once"})
	})
	return ch, cancel
}

type busWithOverflowingSubscriber struct {
	*events.InProcessBus
	uid   ids.UserId
	burst int
	once  sync.Once
}

func (b *busWithOverflowingSubscriber) Subscribe(userId ids.UserId) (<-chan events.Event, func()) {
	ch, cancel := b.InProcessBus.Subscribe(userId)
	b.once.Do(func() {
		for i := 0; i < b.burst; i++ {
			b.Publish(context.Background(), b.uid, events.EventType(fmt.Sprintf("burst-%d", i)), map[string]any{"i": i})
		}
	})
	return ch, cancel
}

const burstBufferedByBus = 16

func TestSSEHandler_DroppedLiveEventsEmitResyncBeforeNextEvent(t *testing.T) {
	real := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())

	overflowing := &busWithOverflowingSubscriber{InProcessBus: real, uid: uid, burst: burstBufferedByBus + 4}
	h := handler.NewEventStreamHandler(overflowing, 0)
	h.WithHeartbeat(time.Hour)
	srv := serveSSE(t, h, uid)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	lastBuffered := fmt.Sprintf("event: burst-%d", burstBufferedByBus-1)
	readUntil(t, br, func(l string) bool { return l == lastBuffered })

	real.Publish(context.Background(), uid, "after", map[string]any{"k": "v"})

	got := readUntil(t, br, func(l string) bool { return l == "event: resync" || l == "event: after" })
	if strings.TrimRight(got, "\n") != "event: resync" {
		t.Fatalf("first event frame after the dropped burst = %q, want event: resync", got)
	}
}

func lastEventIDFor(t *testing.T, bus *events.InProcessBus, uid ids.UserId) string {
	t.Helper()
	ch, cancel := bus.Subscribe(uid)
	defer cancel()
	bus.Publish(context.Background(), uid, "seed", map[string]any{"k": "v"})
	return strconv.FormatUint((<-ch).ID, 10)
}

func serveSSE(t *testing.T, h *handler.EventStreamHandler, uid ids.UserId) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(requestuser.ContextWithUserID(r.Context(), uid))
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestSSEHandler_EventInReplaySubscribeGapIsDelivered(t *testing.T) {
	real := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	lastID := lastEventIDFor(t, real, uid)

	h := handler.NewEventStreamHandler(&busWithGapPublish{InProcessBus: real, uid: uid}, 0)
	h.WithHeartbeat(50 * time.Millisecond)
	srv := serveSSE(t, h, uid)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", lastID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return l == "event: gap" })
}

func TestSSEHandler_ReplayLiveOverlapDedupesByID(t *testing.T) {
	real := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	lastID := lastEventIDFor(t, real, uid)

	h := handler.NewEventStreamHandler(&busWithDupPublish{InProcessBus: real, uid: uid}, 0)
	h.WithHeartbeat(50 * time.Millisecond)
	srv := serveSSE(t, h, uid)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", lastID)

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	real.Publish(context.Background(), uid, "after", map[string]any{"k": "v"})

	br := bufio.NewReader(resp.Body)
	dupCount := 0
	deadline := time.After(2 * time.Second)
	for {
		switch strings.TrimRight(readLineBefore(t, br, deadline), "\n") {
		case "event: dup":
			dupCount++
		case "event: after":
			if dupCount != 1 {
				t.Fatalf("overlapping event delivered %d times, want exactly 1", dupCount)
			}
			return
		}
	}
}

func TestSSEHandler_EmitsHeartbeat(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())

	srv := newTestSSEServer(t, bus, uid, 30*time.Millisecond)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return strings.HasPrefix(l, ":") })
	readUntil(t, br, func(l string) bool { return strings.HasPrefix(l, ":") })
}

type blockingFlushWriter struct {
	header http.Header
	mu     sync.Mutex
	dl     time.Time
	stop   chan struct{}
}

func newBlockingFlushWriter() *blockingFlushWriter {
	return &blockingFlushWriter{header: make(http.Header), stop: make(chan struct{})}
}

func (w *blockingFlushWriter) close() { close(w.stop) }

func (w *blockingFlushWriter) Header() http.Header         { return w.header }
func (w *blockingFlushWriter) WriteHeader(int)             {}
func (w *blockingFlushWriter) Write(p []byte) (int, error) { return len(p), nil }
func (w *blockingFlushWriter) Flush()                      {}

func (w *blockingFlushWriter) SetWriteDeadline(t time.Time) error {
	w.mu.Lock()
	w.dl = t
	w.mu.Unlock()
	return nil
}

func (w *blockingFlushWriter) FlushError() error {
	w.mu.Lock()
	dl := w.dl
	w.mu.Unlock()

	if dl.IsZero() {
		select {
		case <-w.stop:
			return errors.New("stream closed")
		case <-time.After(10 * time.Second):
			return errors.New("flush blocked with no deadline")
		}
	}
	if d := time.Until(dl); d > 0 {
		timer := time.NewTimer(d)
		defer timer.Stop()
		select {
		case <-timer.C:
		case <-w.stop:
			return errors.New("stream closed")
		}
	}
	return os.ErrDeadlineExceeded
}

func TestSSEHandler_WriteDeadlineUnblocksStalledClient(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	h := handler.NewEventStreamHandler(bus, 0)
	h.WithWriteTimeout(150 * time.Millisecond)

	w := newBlockingFlushWriter()
	defer w.close()
	r := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	r = r.WithContext(requestuser.ContextWithUserID(r.Context(), uid))

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(w, r)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return: stalled client blocked the goroutine past the write deadline")
	}
}

func TestSSEHandler_ContextCancelStopsStream(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	h := handler.NewEventStreamHandler(bus, 0)
	h.WithHeartbeat(time.Hour)

	ctx, cancel := context.WithCancel(requestuser.ContextWithUserID(context.Background(), uid))
	r := httptest.NewRequest(http.MethodGet, "/v1/events", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(w, r)
		close(done)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not return after context cancel: shutdown would block")
	}
}

func TestSSEHandler_DefaultPerUserCapRejectsNinthStreamWithAProblem(t *testing.T) {
	uid := ids.NewUserId(uuid.New())
	h := handler.NewEventStreamHandler(events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor")), 0)
	h.WithHeartbeat(time.Hour)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.ServeHTTP(w, r.WithContext(requestuser.ContextWithUserID(r.Context(), uid)))
	}))
	defer srv.Close()

	for i := 0; i < 8; i++ {
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatalf("open stream %d: %v", i+1, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("stream %d status = %d, want 200", i+1, resp.StatusCode)
		}
	}

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("ninth stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("ninth status = %d, want 429", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if got := resp.Header.Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("content type = %q", got)
	}
	if want := `{"type":"urn:altune:problem:eventstream.too_many_streams","title":"Too many event streams","status":429,"detail":"too many event streams","code":"eventstream.too_many_streams"}` + "\n"; string(body) != want {
		t.Fatalf("body = %s, want %s", body, want)
	}
}

func TestSSEHandler_GlobalConnectionCapRejectsOverflowAcrossUsers(t *testing.T) {
	const globalCap = 3
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	h := handler.NewEventStreamHandler(bus, globalCap)
	h.WithHeartbeat(time.Hour)
	srv := newMultiUserSSEServer(t, h)

	users := make([]ids.UserId, globalCap)
	bodies := make([]io.Closer, globalCap)
	readers := make([]*bufio.Reader, globalCap)
	for i := range users {
		users[i] = ids.NewUserId(uuid.New())
		status, body := openUserStream(t, srv.URL, users[i])
		if status != http.StatusOK {
			t.Fatalf("user %d status = %d, want 200 (under the global cap)", i, status)
		}
		bodies[i] = body
		readers[i] = bufio.NewReader(body)
		readUntil(t, readers[i], func(l string) bool { return l == ":ok" })
	}

	status, overflow := openUserStream(t, srv.URL, ids.NewUserId(uuid.New()))
	if status != http.StatusTooManyRequests {
		t.Fatalf("overflow status = %d, want 429 past the global cap", status)
	}
	msg, _ := io.ReadAll(overflow)
	if !strings.Contains(string(msg), `"code":"eventstream.too_many_streams"`) {
		t.Fatalf("overflow body = %q, want the same too-many-streams problem as the per-user cap", msg)
	}

	assertStreamsStillLive(t, bus, users, readers)

	bodies[0].Close()
	waitForSlotRelease(t, srv.URL)
}

func newMultiUserSSEServer(t *testing.T, h *handler.EventStreamHandler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, err := uuid.Parse(r.Header.Get("X-Test-User"))
		if err != nil {
			http.Error(w, "bad test user", http.StatusBadRequest)
			return
		}
		r = r.WithContext(requestuser.ContextWithUserID(r.Context(), ids.NewUserId(uid)))
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func openUserStream(t *testing.T, url string, uid ids.UserId) (int, io.ReadCloser) {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("X-Test-User", uid.String())
	resp, err := http.DefaultClient.Do(req)
	if err != nil || resp == nil {
		t.Fatalf("open stream for %s: %v", uid, err)
		return 0, http.NoBody
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp.StatusCode, resp.Body
}

func assertStreamsStillLive(t *testing.T, bus *events.InProcessBus, users []ids.UserId, readers []*bufio.Reader) {
	t.Helper()
	for i, uid := range users {
		bus.Publish(context.Background(), uid, "still_live", map[string]any{"i": i})
		readUntil(t, readers[i], func(l string) bool { return l == "event: still_live" })
	}
}

func waitForSlotRelease(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, body := openUserStream(t, url, ids.NewUserId(uuid.New()))
		if status == http.StatusOK {
			return
		}
		body.Close()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("closing an open stream never freed a global slot")
}

func serveSSEWithTokenExpiry(t *testing.T, h *handler.EventStreamHandler, uid ids.UserId, expiresAt time.Time) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := requestuser.ContextWithTokenExpiry(requestuser.ContextWithUserID(r.Context(), uid), expiresAt)
		h.ServeHTTP(w, r.WithContext(ctx))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readToEOF(t *testing.T, body io.Reader, within time.Duration) error {
	t.Helper()
	drained := make(chan error, 1)
	go func() {
		_, err := io.Copy(io.Discard, body)
		drained <- err
	}()
	select {
	case err := <-drained:
		return err
	case <-time.After(within):
		return errors.New("stream still open")
	}
}

func TestSSEHandler_StreamEndsAtTokenExpiry(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	h := handler.NewEventStreamHandler(bus, 0)
	h.WithHeartbeat(20 * time.Millisecond)
	srv := serveSSEWithTokenExpiry(t, h, uid, time.Now().Add(300*time.Millisecond))

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return strings.HasPrefix(l, ":ok") })

	if err := readToEOF(t, br, 2*time.Second); err != nil {
		t.Fatalf("stream outlived the token that authenticated it: %v", err)
	}
}

func TestSSEHandler_StreamEndingAtTokenExpiryFreesItsSlot(t *testing.T) {
	bus := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	h := handler.NewEventStreamHandler(bus, 1)
	srv := serveSSEWithTokenExpiry(t, h, uid, time.Now().Add(200*time.Millisecond))

	first, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer first.Body.Close()
	if err := readToEOF(t, first.Body, 2*time.Second); err != nil {
		t.Fatalf("first stream: %v", err)
	}

	second, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer second.Body.Close()
	if second.StatusCode != http.StatusOK {
		t.Fatalf("reconnect status = %d, want 200: the expired stream kept its slot", second.StatusCode)
	}
}

func TestShutdown_SSEStreamEndsOnShutdownSignal(t *testing.T) {
	uid := ids.NewUserId(uuid.New())
	done := make(chan struct{})
	h := handler.NewEventStreamHandler(events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor")), 0).WithShutdown(done)
	h.WithHeartbeat(time.Hour)
	r := httptest.NewRequest(http.MethodGet, "/v1/events", nil).
		WithContext(requestuser.ContextWithUserID(context.Background(), uid))

	returned := make(chan struct{})
	go func() {
		h.ServeHTTP(httptest.NewRecorder(), r)
		close(returned)
	}()
	time.Sleep(20 * time.Millisecond)
	close(done)

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not end on shutdown signal")
	}
}

func TestSSEHandler_DroppedTailEventResyncsAtNextHeartbeat(t *testing.T) {
	real := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())

	overflowing := &busWithOverflowingSubscriber{InProcessBus: real, uid: uid, burst: burstBufferedByBus + 4}
	h := handler.NewEventStreamHandler(overflowing, 0)
	h.WithHeartbeat(50 * time.Millisecond)
	srv := serveSSE(t, h, uid)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return l == "event: resync" })
}

func TestSSEHandler_CaughtUpClientGetsNoResyncAtHeartbeat(t *testing.T) {
	real := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	srv := newTestSSEServer(t, real, uid, 20*time.Millisecond)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return l == ":ok" })
	real.Publish(context.Background(), uid, "one", map[string]any{"k": "v"})
	readUntil(t, br, func(l string) bool { return l == "event: one" })

	pings := 0
	for pings < 3 {
		line := readUntil(t, br, func(l string) bool { return l == ":ping" || l == "event: resync" })
		if strings.TrimRight(line, "\n") == "event: resync" {
			t.Fatal("caught-up client received resync")
		}
		pings++
	}
}

func TestSSEHandler_FreshConnectWithHistoryGetsNoResyncAtHeartbeat(t *testing.T) {
	real := events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor"))
	uid := ids.NewUserId(uuid.New())
	for i := 0; i < 5; i++ {
		real.Publish(context.Background(), uid, "old", map[string]any{"i": i})
	}
	srv := newTestSSEServer(t, real, uid, 20*time.Millisecond)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return l == ":ok" })

	pings := 0
	for pings < 3 {
		line := readUntil(t, br, func(l string) bool { return l == ":ping" || l == "event: resync" })
		if strings.TrimRight(line, "\n") == "event: resync" {
			t.Fatal("fresh connect with prior history received resync")
		}
		pings++
	}
}

func TestSSEHandler_RequestWithoutUserIsRejected(t *testing.T) {
	h := handler.NewEventStreamHandler(events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor")), 0)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/events", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got == "text/event-stream" {
		t.Fatal("unauthenticated request received an event-stream response")
	}
}

type nonFlushingWriter struct {
	header http.Header
	status int
}

func (w *nonFlushingWriter) Header() http.Header         { return w.header }
func (w *nonFlushingWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *nonFlushingWriter) WriteHeader(status int)      { w.status = status }

func TestSSEHandler_WriterWithoutFlushIsRejected(t *testing.T) {
	h := handler.NewEventStreamHandler(events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor")), 1).WithHeartbeat(time.Hour)
	w := &nonFlushingWriter{header: http.Header{}}
	uid := ids.NewUserId(uuid.New())
	r := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	r = r.WithContext(requestuser.ContextWithUserID(r.Context(), uid))

	h.ServeHTTP(w, r)

	if w.status != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.status)
	}
	resp, err := http.Get(serveSSE(t, h, uid).URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("stream after the rejection = %d, want 200: the rejected request held the only slot", resp.StatusCode)
	}
}

func TestSSEHandler_WriterWithoutFlushGetsAnInternalProblem(t *testing.T) {
	h := handler.NewEventStreamHandler(events.NewInProcessBus(time.Now, filepath.Join(t.TempDir(), "event-id-floor")), 0)
	uid := ids.NewUserId(uuid.New())
	r := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	r = r.WithContext(requestuser.ContextWithUserID(r.Context(), uid))
	rec := httptest.NewRecorder()
	w := &bodyCapturingWriter{rec: rec}

	h.ServeHTTP(w, r)

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "application/problem+json" {
		t.Fatalf("content type = %q", got)
	}
	if want := `{"type":"urn:altune:problem:eventstream.streaming_unsupported","title":"Streaming unsupported","status":500,"detail":"streaming not supported","code":"eventstream.streaming_unsupported"}` + "\n"; rec.Body.String() != want {
		t.Fatalf("body = %s, want %s", rec.Body.String(), want)
	}
}

type bodyCapturingWriter struct{ rec *httptest.ResponseRecorder }

func (w *bodyCapturingWriter) Header() http.Header         { return w.rec.Header() }
func (w *bodyCapturingWriter) Write(b []byte) (int, error) { return w.rec.Write(b) }
func (w *bodyCapturingWriter) WriteHeader(status int)      { w.rec.WriteHeader(status) }

func TestSSEHandler_ResumeAfterUserStateLostResyncsWithNothingPublished(t *testing.T) {
	floorPath := filepath.Join(t.TempDir(), "event-id-floor")
	bus := events.NewInProcessBus(time.Now, floorPath)
	uid := ids.NewUserId(uuid.New())
	lastID := lastEventIDFor(t, bus, uid)
	restarted := events.NewInProcessBus(time.Now, floorPath)

	srv := newTestSSEServer(t, restarted, uid, time.Hour)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", lastID)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	readUntil(t, bufio.NewReader(resp.Body), func(l string) bool { return l == "event: resync" })
}
