package app

import (
	"bufio"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
)

// newTestSSEServer wraps the sseHandler in an httptest server that injects a
// fixed authenticated user id, so the handler can be exercised end-to-end over a
// real HTTP stream.
func newTestSSEServer(t *testing.T, bus events.Bus, uid shared.UserId, heartbeat time.Duration) *httptest.Server {
	t.Helper()
	h := &sseHandler{bus: bus, heartbeat: heartbeat}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = r.WithContext(auth.ContextWithUserID(r.Context(), uid))
		h.ServeHTTP(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

// readUntil reads lines from the stream until one satisfies match or the
// deadline elapses. Returns the matching line, or fails the test.
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

// TestSSEHandler_CaughtUpReconnectStreamsLiveEvents is the F1 regression: a
// reconnect whose Last-Event-ID is already caught up must NOT return 204 — it
// must hold the stream open and deliver subsequently-published events.
func TestSSEHandler_CaughtUpReconnectStreamsLiveEvents(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	bus.Publish(uid, "seed", map[string]any{"k": "v"}) // nextID -> 1

	srv := newTestSSEServer(t, bus, uid, 50*time.Millisecond)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", "1") // caught up: nothing newer than id 1

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (must not 204 on caught-up reconnect)", resp.StatusCode)
	}

	br := bufio.NewReader(resp.Body)
	// The initial flush proves the handler reached Subscribe rather than 204ing.
	readUntil(t, br, func(l string) bool { return strings.HasPrefix(l, ":") })

	// A live event published now must reach this open stream.
	bus.Publish(uid, "live", map[string]any{"hello": "world"})
	readUntil(t, br, func(l string) bool { return l == "event: live" })
}

// TestSSEHandler_ReplayGapEmitsResync is the F4 regression: when the client
// resumes from a cursor the ring can no longer cover (here, an id from a
// previous, epoch-lower process), the handler emits a resync control event
// rather than streaming a hole-y partial history.
func TestSSEHandler_ReplayGapEmitsResync(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	bus.Publish(uid, "seed", map[string]any{"k": "v"}) // epoch-seeded id, far above 1

	srv := newTestSSEServer(t, bus, uid, 50*time.Millisecond)

	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Last-Event-ID", "1") // a stale cursor the ring can't cover

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()

	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return l == "event: resync" })
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

// TestSSEHandler_EmitsHeartbeat is the F2 regression: an idle stream must emit
// periodic comment pings so proxies don't silently drop the socket and the
// client watchdog has a signal.
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
	// First comment is the initial flush; a second proves a heartbeat fired.
	readUntil(t, br, func(l string) bool { return strings.HasPrefix(l, ":") })
	readUntil(t, br, func(l string) bool { return strings.HasPrefix(l, ":") })
}
