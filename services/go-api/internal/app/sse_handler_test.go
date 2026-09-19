package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/logging"
	"bufio"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
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
