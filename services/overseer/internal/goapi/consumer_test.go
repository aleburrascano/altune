package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"sync"
	"testing"
	"time"
)

// fastBackoff makes reconnect timing deterministic: every attempt waits a fixed
// tiny interval, so tests exercise the reconnect path without sleeping long or
// flaking on wall-clock timing. It records attempts for assertions.
type fastBackoff struct {
	mu    sync.Mutex
	calls int
	wait  time.Duration
}

func (b *fastBackoff) Backoff(int) time.Duration {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls++
	return b.wait
}

func (b *fastBackoff) attempts() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.calls
}

// stubSSE is a controllable operator SSE endpoint. It streams the configured
// events per connection, and in "down" mode hijacks and drops the connection
// before any response — a genuine transport failure, so the consumer must report
// source-down. Mode flips are how a test forces a disconnect and a recovery.
type stubSSE struct {
	mu       sync.Mutex
	down     bool
	conns    int
	holdOpen bool
	events   []goapi.Event
}

func (s *stubSSE) setDown(down bool) {
	s.mu.Lock()
	s.down = down
	s.mu.Unlock()
}

func (s *stubSSE) snapshot() (down bool, evs []goapi.Event, hold bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.down, append([]goapi.Event(nil), s.events...), s.holdOpen
}

func (s *stubSSE) connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns
}

func (s *stubSSE) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	down, evs, hold := s.snapshot()
	if down {
		hijackClose(w)
		return
	}
	s.mu.Lock()
	s.conns++
	s.mu.Unlock()
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "no flush", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	for _, ev := range evs {
		writeEvent(w, ev)
		flusher.Flush()
	}
	if hold {
		<-r.Context().Done() // keep the stream open until the client disconnects
	}
	// Returning here closes the stream, so the consumer sees EOF and reconnects.
}

func hijackClose(w http.ResponseWriter) {
	hj, ok := w.(http.Hijacker)
	if !ok {
		return
	}
	conn, _, err := hj.Hijack()
	if err != nil {
		return
	}
	_ = conn.Close()
}

func writeEvent(w io.Writer, ev goapi.Event) {
	b, err := json.Marshal(ev)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}

func newConsumer(t *testing.T, baseURL string) (*goapi.Consumer, *fastBackoff) {
	t.Helper()
	bo := &fastBackoff{wait: time.Millisecond}
	c, err := goapi.NewConsumer(baseURL, goapi.StaticTokenSource(testToken), goapi.WithBackoff(bo))
	if err != nil {
		t.Fatalf("NewConsumer(%q): %v", baseURL, err)
	}
	return c, bo
}

// recv waits for one event or fails the test, so a broken stream can never hang
// the suite.
func recv(t *testing.T, ch <-chan goapi.Event) goapi.Event {
	t.Helper()
	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("events channel closed while awaiting an event")
		}
		return ev
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for an event")
		return goapi.Event{}
	}
}

func eventually(t *testing.T, why string, cond func() bool) {
	t.Helper()
	eventuallyWithin(t, why, 2*time.Second, cond)
}

func eventuallyWithin(t *testing.T, why string, timeout time.Duration, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("condition never held: %s", why)
}

// TestConsumerReceivesDecodedEvents is the core Done proof: the consumer connects
// to a stubbed operator SSE endpoint and yields decoded events on its channel.
func TestConsumerReceivesDecodedEvents(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	stub := &stubSSE{
		holdOpen: true,
		events: []goapi.Event{
			{Type: "track.played", Timestamp: when, User: "u1", Subject: "song a"},
			{Type: "search.performed", Timestamp: when, Subject: "jazz"},
		},
	}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	got := recv(t, c.Events())
	if got.Type != "track.played" || got.User != "u1" || got.Subject != "song a" {
		t.Fatalf("event 1 = %+v, want track.played/u1/song a", got)
	}
	if !got.Timestamp.Equal(when) {
		t.Fatalf("event 1 timestamp = %v, want %v", got.Timestamp, when)
	}
	if got2 := recv(t, c.Events()); got2.Type != "search.performed" || got2.Subject != "jazz" {
		t.Fatalf("event 2 = %+v, want search.performed/jazz", got2)
	}
	eventually(t, "status up while streaming", func() bool { return c.Status() == goapi.StatusUp })
}

// TestConsumerReconnectsWithBackoff proves the consumer survives a forced
// disconnect: each connection streams one event then closes, and the consumer
// reconnects (via the injected backoff) to receive the next — resuming the stream
// across go-api restarts.
func TestConsumerReconnectsWithBackoff(t *testing.T) {
	when := time.Now().UTC()
	stub := &stubSSE{events: []goapi.Event{{Type: "heartbeat", Timestamp: when}}}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, bo := newConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	for i := 0; i < 3; i++ {
		if ev := recv(t, c.Events()); ev.Type != "heartbeat" {
			t.Fatalf("event %d = %+v, want heartbeat", i, ev)
		}
	}
	eventually(t, "reconnected across multiple connections", func() bool { return stub.connections() >= 2 })
	if bo.attempts() == 0 {
		t.Fatal("backoff was never consulted; reconnect did not use the backoff seam")
	}
}

// TestConsumerReportsSourceDownAndRecovers is the spine primitive: drop the
// upstream and the consumer reports connecting (not down) while it keeps
// retrying within the reconnect grace, only falls to source-down once repeated
// reconnects fail past that grace, and recovers to StatusUp and resumes events
// once a connection succeeds again.
func TestConsumerReportsSourceDownAndRecovers(t *testing.T) {
	when := time.Now().UTC()
	stub := &stubSSE{holdOpen: true, events: []goapi.Event{{Type: "alive", Timestamp: when}}}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	if ev := recv(t, c.Events()); ev.Type != "alive" {
		t.Fatalf("first event = %+v, want alive", ev)
	}
	eventually(t, "status up before the drop", func() bool { return c.Status() == goapi.StatusUp })

	stub.setDown(true)           // future connects fail at the transport (hijack + close)
	srv.CloseClientConnections() // and drop the live connection so the consumer must reconnect
	eventually(t, "status connecting right after the drop", func() bool { return c.Status() == goapi.StatusConnecting })
	eventuallyWithin(t, "status down once reconnects fail past the 10s grace", 15*time.Second, func() bool {
		return c.Status() == goapi.StatusDown
	})
	if err := c.LastError(); !goapi.IsSourceDown(err) {
		t.Fatalf("LastError = %v (%T), want a source-down error while down", err, err)
	}

	stub.setDown(false) // upstream returns
	eventually(t, "status up after recovery", func() bool { return c.Status() == goapi.StatusUp })
	if ev := recv(t, c.Events()); ev.Type != "alive" {
		t.Fatalf("post-recovery event = %+v, want alive", ev)
	}
}

// TestConsumerNon200IsAPIErrorNotSourceDown proves a reachable-but-rejecting
// go-api (e.g. a 502 mid-deploy) still flips the status down, but keeps the typed
// distinction: the recorded error is an APIError, not source-down.
func TestConsumerNon200IsAPIErrorNotSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte("upstream down"))
	}))
	defer srv.Close()

	c, _ := newConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	eventually(t, "status down on 502", func() bool { return c.Status() == goapi.StatusDown })
	err := c.LastError()
	if goapi.IsSourceDown(err) {
		t.Fatalf("502 misclassified as source-down: %v", err)
	}
	var apiErr *goapi.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadGateway {
		t.Fatalf("LastError = %v (%T), want APIError 502", err, err)
	}
}

// TestConsumerHungConnectIsSourceDown proves a go-api that accepts the socket but
// never sends response headers cannot wedge the consumer: the response-header
// timeout fires and the attempt surfaces as source-down, then keeps retrying.
func TestConsumerHungConnectIsSourceDown(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		select {
		case <-block:
		case <-r.Context().Done():
		}
	}))
	defer srv.Close()
	defer close(block)

	hc := &http.Client{Transport: &http.Transport{ResponseHeaderTimeout: 50 * time.Millisecond}}
	bo := &fastBackoff{wait: time.Millisecond}
	c, err := goapi.NewConsumer(srv.URL, goapi.StaticTokenSource(testToken),
		goapi.WithConsumerHTTPClient(hc), goapi.WithBackoff(bo))
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := runConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	eventually(t, "hung connect reported source-down", func() bool {
		return c.Status() == goapi.StatusDown && goapi.IsSourceDown(c.LastError())
	})
}

// TestConsumerRunOnce proves a Consumer runs at most once: a second Run is
// rejected rather than racing a second producer onto the events channel.
func TestConsumerRunOnce(t *testing.T) {
	srv := httptest.NewServer(&stubSSE{holdOpen: true})
	defer srv.Close()

	c, _ := newConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	eventually(t, "first Run took the started guard", func() bool { return c.Status() != goapi.StatusConnecting })
	if err := c.Run(context.Background()); err == nil {
		t.Fatal("second Run returned nil; expected an already-running error")
	}
}

// TestConsumerShutdownClosesEventsNoLeak proves clean shutdown: cancelling ctx
// returns Run with ctx.Err, closes the events channel, and leaks no goroutine
// per connection (the reconnect/stream watcher pattern).
func TestConsumerShutdownClosesEventsNoLeak(t *testing.T) {
	stub := &stubSSE{events: []goapi.Event{{Type: "tick", Timestamp: time.Now().UTC()}}}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newConsumer(t, srv.URL)
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- c.Run(ctx) }()

	// Let it reconnect a few times so any per-connection goroutine leak accrues.
	eventually(t, "several reconnects happened", func() bool { return stub.connections() >= 3 })
	cancel()

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("Run returned nil on shutdown; want ctx error")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after ctx cancellation")
	}
	drainUntilClosed(t, c.Events())
	eventually(t, "goroutines settle back near the pre-Run baseline", func() bool {
		runtime.GC()
		return runtime.NumGoroutine() <= before+3
	})
}

// TestNewConsumerValidatesConfig rejects misconfiguration at construction.
func TestNewConsumerValidatesConfig(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		tokens  goapi.TokenSource
	}{
		{"empty base url", "", goapi.StaticTokenSource(testToken)},
		{"no scheme", "api.altune.app", goapi.StaticTokenSource(testToken)},
		{"nil token source", "https://api.altune.app", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := goapi.NewConsumer(tc.baseURL, tc.tokens); err == nil {
				t.Fatalf("NewConsumer(%q) returned nil error, want rejection", tc.baseURL)
			}
		})
	}
}

func runConsumer(t *testing.T, c *goapi.Consumer, ctx context.Context) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Run(ctx)
	}()
	return done
}

// drainUntilClosed reads any buffered events and asserts the channel is closed on
// shutdown (a closed channel yields ok=false), so a range over Events() ends.
func drainUntilClosed(t *testing.T, ch <-chan goapi.Event) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
		case <-timeout:
			t.Fatal("events channel not closed after shutdown")
		}
	}
}

func TestConsumerLastErrorClearsOnRecovery(t *testing.T) {
	stub := &stubSSE{holdOpen: true, events: []goapi.Event{{Type: "alive", Timestamp: time.Now().UTC()}}}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	recv(t, c.Events())
	eventually(t, "status up before the drop", func() bool { return c.Status() == goapi.StatusUp })

	stub.setDown(true)
	srv.CloseClientConnections()
	eventually(t, "status connecting right after the drop", func() bool { return c.Status() == goapi.StatusConnecting })
	eventuallyWithin(t, "status down once reconnects fail past the 10s grace", 15*time.Second, func() bool {
		return c.Status() == goapi.StatusDown
	})
	if c.LastError() == nil {
		t.Fatal("LastError = nil while down, want the drop's error")
	}

	stub.setDown(false)
	eventually(t, "status up after recovery", func() bool { return c.Status() == goapi.StatusUp })
	recv(t, c.Events())
	if err := c.LastError(); err != nil {
		t.Fatalf("LastError after recovery = %v, want nil", err)
	}
}
