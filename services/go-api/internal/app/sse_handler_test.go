package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestSSEServer(t *testing.T, bus *events.InProcessBus, uid shared.UserId, heartbeat time.Duration) *httptest.Server {
	t.Helper()
	h := newSSEHandler(bus, 0)
	h.heartbeat = heartbeat
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(auth.ContextWithUserID(r.Context(), uid))
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func readUntil(t *testing.T, r *bufio.Reader, match func(string) bool) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, err := r.ReadString('\n')
		if err != nil {
			t.Fatalf("reading stream: %v", err)
		}
		if match(strings.TrimRight(line, "\n")) {
			return line
		}
	}
	t.Fatal("timed out waiting for expected line")
	return ""
}

func TestSSEHandler_CaughtUpReconnectStreamsLiveEvents(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
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
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
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
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())

	srv := newTestSSEServer(t, bus, uid, 50*time.Millisecond)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return strings.HasPrefix(l, ":") })

	// A channel cannot be JSON-marshalled: without a resync signal this event
	// vanishes silently and the client never learns it missed state.
	bus.Publish(context.Background(), uid, "unmarshalable", map[string]any{"bad": make(chan int)})
	readUntil(t, br, func(l string) bool { return l == "event: resync" })
}

func TestSSEHandler_MalformedLastEventIDEmitsResync(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	bus.Publish(context.Background(), uid, "seed", map[string]any{"k": "v"})

	srv := newTestSSEServer(t, bus, uid, 50*time.Millisecond)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A non-numeric Last-Event-ID cannot be parsed: without a resync signal the
	// client silently resumes live-only, believing its replay is intact.
	req.Header.Set("Last-Event-ID", "not-a-number")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return l == "event: resync" })
}

// TestSSEHandler_OutOfRangeLastEventIDResyncsAndStreamsLive is the regression
// guard for #1013: a Last-Event-ID beyond anything the bus has issued (a stale
// or corrupted header) must force a resync, and later live events must still
// reach the client instead of being deduped against the bogus ID forever.
func TestSSEHandler_OutOfRangeLastEventIDResyncsAndStreamsLive(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
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

// TestSSEHandler_LastEventIDAtHighWaterMarkIsCaughtUp pins the boundary: the
// most recently issued ID is a legitimate caught-up resume, not a gap.
func TestSSEHandler_LastEventIDAtHighWaterMarkIsCaughtUp(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
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

// waitForLog polls the ring buffer until a record with the given message is
// captured, so an assertion does not race the handler goroutine that logs it.
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

// TestSSEHandler_ConnectDisconnectLogsCarryCorrelationID is the regression
// guard for #372: the connect and disconnect log lines must carry the request's
// correlation ID so a client-reported ID can be grepped server-side. Both calls
// use the *Context slog variant with the request context, so the correlation
// handler stamps corr_id automatically like RequestLogger.
func TestSSEHandler_ConnectDisconnectLogsCarryCorrelationID(t *testing.T) {
	prev := slog.Default()
	defer slog.SetDefault(prev)
	ring := logging.Setup("debug", false)

	const corrID = "corr-sse-1234"
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	h := newSSEHandler(bus, 0)
	h.heartbeat = 50 * time.Millisecond
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := logging.WithCorrelationID(auth.ContextWithUserID(r.Context(), uid), corrID)
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

	// Cancelling the client request cancels the server's request context, which
	// ends the stream and triggers the sse.disconnected log line.
	cancel()
	resp.Body.Close()

	if got := waitForLog(t, ring, "sse.connected").Attrs["corr_id"]; got != corrID {
		t.Errorf("sse.connected corr_id = %q, want %q", got, corrID)
	}
	if got := waitForLog(t, ring, "sse.disconnected").Attrs["corr_id"]; got != corrID {
		t.Errorf("sse.disconnected corr_id = %q, want %q", got, corrID)
	}
}

func TestReplayGapped(t *testing.T) {
	tests := []struct {
		name     string
		replayed []events.Event
		afterID  uint64
		want     bool
	}{
		{name: "empty replay is not a gap", replayed: nil, afterID: 5, want: false},
		{name: "contiguous tail is not a gap", replayed: []events.Event{{ID: 6}}, afterID: 5, want: false},
		{name: "skipped tail is a gap", replayed: []events.Event{{ID: 9}}, afterID: 5, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := replayGapped(tt.replayed, tt.afterID); got != tt.want {
				t.Errorf("replayGapped(%v, %d) = %v, want %v", tt.replayed, tt.afterID, got, tt.want)
			}
		})
	}
}

func TestSSEStreamGapped(t *testing.T) {
	tests := []struct {
		name               string
		deliveredThroughID uint64
		nextID             uint64
		want               bool
	}{
		{name: "nothing delivered yet is not a gap", deliveredThroughID: 0, nextID: 900, want: false},
		{name: "consecutive id is not a gap", deliveredThroughID: 5, nextID: 6, want: false},
		{name: "one skipped id is a gap", deliveredThroughID: 5, nextID: 7, want: true},
		{name: "a whole dropped burst is a gap", deliveredThroughID: 5, nextID: 25, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := streamGapped(tt.deliveredThroughID, tt.nextID); got != tt.want {
				t.Errorf("streamGapped(%d, %d) = %v, want %v", tt.deliveredThroughID, tt.nextID, got, tt.want)
			}
		})
	}
}

// busWithGapPublish wraps a real bus and publishes one event the instant a
// replay snapshot is taken, landing it squarely in the replay/subscribe window.
// A handler that replays THEN subscribes delivers it to neither and drops it
// silently (#371); a subscribe-first handler catches it on the live channel.
type busWithGapPublish struct {
	*events.InProcessBus
	uid  shared.UserId
	once sync.Once
}

func (b *busWithGapPublish) Replay(userId shared.UserId, afterID uint64) []events.Event {
	snapshot := b.InProcessBus.Replay(userId, afterID)
	b.once.Do(func() {
		b.Publish(context.Background(), b.uid, "gap", map[string]any{"in": "window"})
	})
	return snapshot
}

// busWithDupPublish publishes one event at subscribe time, so it lands in BOTH
// the live channel and the subsequent replay snapshot. The handler must emit it
// exactly once, deduping by event ID, not twice.
type busWithDupPublish struct {
	*events.InProcessBus
	uid  shared.UserId
	once sync.Once
}

func (b *busWithDupPublish) Subscribe(userId shared.UserId) (<-chan events.Event, func()) {
	ch, cancel := b.InProcessBus.Subscribe(userId)
	b.once.Do(func() {
		b.Publish(context.Background(), b.uid, "dup", map[string]any{"seen": "once"})
	})
	return ch, cancel
}

// busWithOverflowingSubscriber floods a subscriber's channel the moment it is
// handed out, before the handler can drain a single event. The bus buffers 16
// and drops the rest on the floor, so the events after burstBufferedByBus never
// reach the client while the connection stays perfectly healthy.
type busWithOverflowingSubscriber struct {
	*events.InProcessBus
	uid   shared.UserId
	burst int
	once  sync.Once
}

func (b *busWithOverflowingSubscriber) Subscribe(userId shared.UserId) (<-chan events.Event, func()) {
	ch, cancel := b.InProcessBus.Subscribe(userId)
	b.once.Do(func() {
		for i := 0; i < b.burst; i++ {
			b.Publish(context.Background(), b.uid, fmt.Sprintf("burst-%d", i), map[string]any{"i": i})
		}
	})
	return ch, cancel
}

// burstBufferedByBus mirrors the unexported events.subscriberChanSize: the
// number of events a subscriber channel holds before Publish starts dropping.
const burstBufferedByBus = 16

// TestSSEHandler_DroppedLiveEventsEmitResyncBeforeNextEvent is the regression
// guard for #2016: when the bus drops events for a full subscriber channel the
// client is never disconnected, so it never replays via Last-Event-ID. The next
// event to get through must carry a resync ahead of it, or the client keeps
// serving state it does not know is stale.
func TestSSEHandler_DroppedLiveEventsEmitResyncBeforeNextEvent(t *testing.T) {
	real := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())

	overflowing := &busWithOverflowingSubscriber{InProcessBus: real, uid: uid, burst: burstBufferedByBus + 4}
	h := newSSEHandler(overflowing, 0)
	h.heartbeat = time.Hour
	srv := serveSSE(t, h, uid)

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	// Draining the buffered prefix frees the channel, so the sentinel published
	// next is guaranteed a slot and arrives separated by the 4 dropped IDs.
	br := bufio.NewReader(resp.Body)
	lastBuffered := fmt.Sprintf("event: burst-%d", burstBufferedByBus-1)
	readUntil(t, br, func(l string) bool { return l == lastBuffered })

	real.Publish(context.Background(), uid, "after", map[string]any{"k": "v"})

	got := readUntil(t, br, func(l string) bool { return l == "event: resync" || l == "event: after" })
	if strings.TrimRight(got, "\n") != "event: resync" {
		t.Fatalf("first event frame after the dropped burst = %q, want event: resync", got)
	}
}

func lastEventIDFor(t *testing.T, bus *events.InProcessBus, uid shared.UserId) string {
	t.Helper()
	ch, cancel := bus.Subscribe(uid)
	defer cancel()
	bus.Publish(context.Background(), uid, "seed", map[string]any{"k": "v"})
	return strconv.FormatUint((<-ch).ID, 10)
}

func serveSSE(t *testing.T, h *sseHandler, uid shared.UserId) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(auth.ContextWithUserID(r.Context(), uid))
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestSSEHandler_EventInReplaySubscribeGapIsDelivered is the regression guard
// for #371: an event published between the replay snapshot and the live
// subscribe must still reach the client exactly once, never silently dropped.
func TestSSEHandler_EventInReplaySubscribeGapIsDelivered(t *testing.T) {
	real := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	lastID := lastEventIDFor(t, real, uid)

	h := newSSEHandler(&busWithGapPublish{InProcessBus: real, uid: uid}, 0)
	h.heartbeat = 50 * time.Millisecond
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

// TestSSEHandler_ReplayLiveOverlapDedupesByID proves the subscribe-first fix
// does not double-deliver: an event present in both the replay snapshot and the
// live channel is written once, deduped by event ID.
func TestSSEHandler_ReplayLiveOverlapDedupesByID(t *testing.T) {
	real := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	lastID := lastEventIDFor(t, real, uid)

	h := newSSEHandler(&busWithDupPublish{InProcessBus: real, uid: uid}, 0)
	h.heartbeat = 50 * time.Millisecond
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

	// The subscriber is registered before the first byte is flushed, so by the
	// time Do returns this sentinel lands on the live channel behind the overlap.
	real.Publish(context.Background(), uid, "after", map[string]any{"k": "v"})

	br := bufio.NewReader(resp.Body)
	dupCount := 0
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		line, err := br.ReadString('\n')
		if err != nil {
			t.Fatalf("reading stream: %v", err)
		}
		switch strings.TrimRight(line, "\n") {
		case "event: dup":
			dupCount++
		case "event: after":
			if dupCount != 1 {
				t.Fatalf("overlapping event delivered %d times, want exactly 1", dupCount)
			}
			return
		}
	}
	t.Fatal("timed out waiting for sentinel event")
}

func TestSSEHandler_EmitsHeartbeat(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())

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

// blockingFlushWriter models a client that has stopped reading: writes buffer
// fine but the flush to the socket blocks until the write deadline fires.
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
		// No deadline was set: emulate a flush that blocks on a non-reading
		// client forever. Bounded by stop so a broken handler can't truly hang.
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

// Gap 1: a stalled client must not pin the handler goroutine forever. The
// per-write deadline has to unblock the flush and tear the stream down.
func TestSSEHandler_WriteDeadlineUnblocksStalledClient(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	h := newSSEHandler(bus, 0)
	h.writeTimeout = 150 * time.Millisecond

	w := newBlockingFlushWriter()
	defer w.close()
	r := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	r = r.WithContext(auth.ContextWithUserID(r.Context(), uid))

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

// Gap 2: shutdown cancels the lifecycle context (wired as the server
// BaseContext), which must propagate to r.Context() and stop the stream.
func TestSSEHandler_ContextCancelStopsStream(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	h := newSSEHandler(bus, 0)
	h.heartbeat = time.Hour

	ctx, cancel := context.WithCancel(auth.ContextWithUserID(context.Background(), uid))
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

// Gap 3: a single user cannot open unbounded concurrent streams; the overflow
// connection is handled with 429, not accepted and not fatal.
func TestSSEHandler_PerUserConnectionCapRejectsOverflow(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	h := newSSEHandler(bus, 0)
	h.limiter = newConnLimiter(2, 0)
	h.heartbeat = time.Hour

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(auth.ContextWithUserID(r.Context(), uid))
		h.ServeHTTP(w, r)
	}))
	defer srv.Close()

	for i := 0; i < 2; i++ {
		resp, err := http.Get(srv.URL)
		if err != nil {
			t.Fatalf("open stream %d: %v", i, err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("stream %d status = %d, want 200", i, resp.StatusCode)
		}
	}

	resp, err := http.Get(srv.URL)
	if err != nil {
		t.Fatalf("overflow stream: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusTooManyRequests {
		t.Fatalf("overflow status = %d, want 429", resp.StatusCode)
	}
}

func TestConnLimiter(t *testing.T) {
	l := newConnLimiter(2, 0)
	if err := l.acquire("u"); err != nil {
		t.Fatalf("first acquire should succeed: %v", err)
	}
	if err := l.acquire("u"); err != nil {
		t.Fatalf("second acquire should succeed: %v", err)
	}
	if err := l.acquire("u"); !errors.Is(err, errUserConnLimit) {
		t.Fatalf("third acquire = %v, want errUserConnLimit at the cap", err)
	}
	l.release("u")
	if err := l.acquire("u"); err != nil {
		t.Fatalf("acquire should succeed after a release frees a slot: %v", err)
	}
	if err := l.acquire("v"); err != nil {
		t.Fatalf("a different user must have an independent budget: %v", err)
	}
}

// #1022: the global ceiling counts slots across all keys, a per-user rejection
// reserves nothing, and a stray release cannot free phantom global capacity.
func TestConnLimiter_GlobalCapAcrossUsers(t *testing.T) {
	l := newConnLimiter(1, 2)
	if err := l.acquire("a"); err != nil {
		t.Fatalf("a: %v", err)
	}
	if err := l.acquire("a"); !errors.Is(err, errUserConnLimit) {
		t.Fatalf("a again = %v, want errUserConnLimit", err)
	}
	if err := l.acquire("b"); err != nil {
		t.Fatalf("b must fit: the per-user rejection must not hold a global slot: %v", err)
	}
	if err := l.acquire("c"); !errors.Is(err, errGlobalConnLimit) {
		t.Fatalf("c = %v, want errGlobalConnLimit", err)
	}
	l.release("never-acquired")
	if err := l.acquire("c"); !errors.Is(err, errGlobalConnLimit) {
		t.Fatalf("c after stray release = %v, want errGlobalConnLimit", err)
	}
	l.release("a")
	if err := l.acquire("c"); err != nil {
		t.Fatalf("c after a real release should fit: %v", err)
	}
}

// #1022: parallel connects from distinct users must never overshoot the
// global ceiling.
func TestConnLimiter_GlobalCapHoldsUnderConcurrency(t *testing.T) {
	const limit, attempts = 5, 200
	l := newConnLimiter(defaultMaxConnsPerUser, limit)
	var granted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.acquire(uuid.NewString()) == nil {
				granted.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := granted.Load(); got != limit {
		t.Fatalf("granted %d slots, want exactly %d", got, limit)
	}
}

// #1022: past the server-wide ceiling, a stream from yet another user is shed
// with the same 429 as the per-user cap, while the streams already open keep
// receiving events; closing one frees its slot for a newcomer.
func TestSSEHandler_GlobalConnectionCapRejectsOverflowAcrossUsers(t *testing.T) {
	const globalCap = 3
	bus := events.NewInProcessBus()
	h := newSSEHandler(bus, globalCap)
	h.heartbeat = time.Hour
	srv := newMultiUserSSEServer(t, h)

	users := make([]shared.UserId, globalCap)
	bodies := make([]io.Closer, globalCap)
	readers := make([]*bufio.Reader, globalCap)
	for i := range users {
		users[i] = shared.NewUserId(uuid.New())
		status, body := openUserStream(t, srv.URL, users[i])
		if status != http.StatusOK {
			t.Fatalf("user %d status = %d, want 200 (under the global cap)", i, status)
		}
		bodies[i] = body
		readers[i] = bufio.NewReader(body)
		readUntil(t, readers[i], func(l string) bool { return l == ":ok" })
	}

	status, overflow := openUserStream(t, srv.URL, shared.NewUserId(uuid.New()))
	if status != http.StatusTooManyRequests {
		t.Fatalf("overflow status = %d, want 429 past the global cap", status)
	}
	msg, _ := io.ReadAll(overflow)
	if got := strings.TrimSpace(string(msg)); got != "too many event streams" {
		t.Fatalf("overflow body = %q, want the per-user rejection message", got)
	}

	assertStreamsStillLive(t, bus, users, readers)

	bodies[0].Close()
	waitForSlotRelease(t, srv.URL)
}

func newMultiUserSSEServer(t *testing.T, h *sseHandler) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		uid, err := uuid.Parse(r.Header.Get("X-Test-User"))
		if err != nil {
			http.Error(w, "bad test user", http.StatusBadRequest)
			return
		}
		r = r.WithContext(auth.ContextWithUserID(r.Context(), shared.NewUserId(uid)))
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// openUserStream opens /v1/events as uid and returns the status and body; the
// body is closed at test cleanup.
func openUserStream(t *testing.T, url string, uid shared.UserId) (int, io.ReadCloser) {
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

// assertStreamsStillLive publishes to every open user and reads the event back
// off the wire, proving the overflow rejection disturbed no existing stream.
func assertStreamsStillLive(t *testing.T, bus *events.InProcessBus, users []shared.UserId, readers []*bufio.Reader) {
	t.Helper()
	for i, uid := range users {
		bus.Publish(context.Background(), uid, "still_live", map[string]any{"i": i})
		readUntil(t, readers[i], func(l string) bool { return l == "event: still_live" })
	}
}

// waitForSlotRelease polls until a newcomer is admitted, since the server
// notices the closed client asynchronously.
func waitForSlotRelease(t *testing.T, url string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		status, body := openUserStream(t, url, shared.NewUserId(uuid.New()))
		if status == http.StatusOK {
			return
		}
		body.Close()
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("closing an open stream never freed a global slot")
}
