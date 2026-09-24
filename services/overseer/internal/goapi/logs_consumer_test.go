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
	"strings"
	"sync"
	"testing"
	"time"
)

// stubLogSSE is a controllable operator log SSE endpoint. It streams the
// configured records per connection, and in "down" mode hijacks and drops the
// connection before any response — a genuine transport failure, so the consumer
// must report source-down. It can also emit a raw body (malformed or oversized
// frames) to exercise the decoder's error paths.
type stubLogSSE struct {
	mu       sync.Mutex
	down     bool
	conns    int
	holdOpen bool
	records  []goapi.LogRecord
	rawBody  string
}

func (s *stubLogSSE) setDown(down bool) {
	s.mu.Lock()
	s.down = down
	s.mu.Unlock()
}

func (s *stubLogSSE) snapshot() (down bool, recs []goapi.LogRecord, hold bool, raw string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.down, append([]goapi.LogRecord(nil), s.records...), s.holdOpen, s.rawBody
}

func (s *stubLogSSE) connections() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.conns
}

func (s *stubLogSSE) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	down, recs, hold, raw := s.snapshot()
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
	if raw != "" {
		_, _ = io.WriteString(w, raw)
		flusher.Flush()
	}
	for _, rec := range recs {
		writeLogRecord(w, rec)
		flusher.Flush()
	}
	if hold {
		<-r.Context().Done()
	}
}

func writeLogRecord(w io.Writer, rec goapi.LogRecord) {
	b, err := json.Marshal(rec)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", b)
}

func newLogsConsumer(t *testing.T, baseURL string) (*goapi.LogsConsumer, *fastBackoff) {
	t.Helper()
	bo := &fastBackoff{wait: time.Millisecond}
	c, err := goapi.NewLogsConsumer(baseURL, goapi.StaticTokenSource(testToken), goapi.WithLogsBackoff(bo))
	if err != nil {
		t.Fatalf("NewLogsConsumer(%q): %v", baseURL, err)
	}
	return c, bo
}

func recvLog(t *testing.T, ch <-chan goapi.LogRecord) goapi.LogRecord {
	t.Helper()
	select {
	case rec, ok := <-ch:
		if !ok {
			t.Fatal("records channel closed while awaiting a record")
		}
		return rec
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for a log record")
		return goapi.LogRecord{}
	}
}

func runLogsConsumer(t *testing.T, c *goapi.LogsConsumer, ctx context.Context) <-chan struct{} {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Run(ctx)
	}()
	return done
}

// TestLogsConsumerDecodesRecords is the core Done proof: the consumer connects to
// a stubbed operator log SSE endpoint and yields decoded records — level, message
// and every field — on its channel.
func TestLogsConsumerDecodesRecords(t *testing.T) {
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	stub := &stubLogSSE{
		holdOpen: true,
		records: []goapi.LogRecord{
			{Time: when, Level: "INFO", Message: "queue resumed", Fields: map[string]string{"queue": "q1"}},
			{Time: when, Level: "ERROR", Message: "lookup failed", Fields: map[string]string{"err": "boom", "user": "u1"}},
		},
	}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newLogsConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLogsConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	got := recvLog(t, c.Records())
	if got.Level != "INFO" || got.Message != "queue resumed" || got.Fields["queue"] != "q1" {
		t.Fatalf("record 1 = %+v, want INFO/queue resumed/queue=q1", got)
	}
	if !got.Time.Equal(when) {
		t.Fatalf("record 1 time = %v, want %v", got.Time, when)
	}
	got2 := recvLog(t, c.Records())
	if got2.Level != "ERROR" || got2.Message != "lookup failed" || got2.Fields["err"] != "boom" || got2.Fields["user"] != "u1" {
		t.Fatalf("record 2 = %+v, want ERROR/lookup failed/err=boom user=u1", got2)
	}
	eventually(t, "status up while streaming", func() bool { return c.Status() == goapi.StatusUp })
}

// TestLogsConsumerReconnectsWithBackoff proves the consumer survives a forced
// disconnect: each connection streams one record then closes, and the consumer
// reconnects (via the injected backoff) to receive the next.
func TestLogsConsumerReconnectsWithBackoff(t *testing.T) {
	when := time.Now().UTC()
	stub := &stubLogSSE{records: []goapi.LogRecord{{Time: when, Level: "INFO", Message: "tick"}}}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, bo := newLogsConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLogsConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	for i := 0; i < 3; i++ {
		if rec := recvLog(t, c.Records()); rec.Message != "tick" {
			t.Fatalf("record %d = %+v, want tick", i, rec)
		}
	}
	eventually(t, "reconnected across multiple connections", func() bool { return stub.connections() >= 2 })
	if bo.attempts() == 0 {
		t.Fatal("backoff was never consulted; reconnect did not use the backoff seam")
	}
}

// TestLogsConsumerReportsSourceDownAndRecovers is the spine primitive: drop the
// upstream and the consumer reports connecting (not source-down) while it
// retries within the reconnect grace, only falls to source-down (typed
// SourceDownError, not a panic) once repeated reconnects fail past that grace,
// then recovers to StatusUp and resumes records on reconnect.
func TestLogsConsumerReportsSourceDownAndRecovers(t *testing.T) {
	when := time.Now().UTC()
	stub := &stubLogSSE{holdOpen: true, records: []goapi.LogRecord{{Time: when, Level: "INFO", Message: "alive"}}}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newLogsConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLogsConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	if rec := recvLog(t, c.Records()); rec.Message != "alive" {
		t.Fatalf("first record = %+v, want alive", rec)
	}
	eventually(t, "status up before the drop", func() bool { return c.Status() == goapi.StatusUp })

	stub.setDown(true)
	srv.CloseClientConnections()
	eventually(t, "status connecting right after the drop", func() bool { return c.Status() == goapi.StatusConnecting })
	eventuallyWithin(t, "status down once reconnects fail past the 10s grace", 15*time.Second, func() bool {
		return c.Status() == goapi.StatusDown
	})
	if err := c.LastError(); !goapi.IsSourceDown(err) {
		t.Fatalf("LastError = %v (%T), want a source-down error while down", err, err)
	}

	stub.setDown(false)
	eventually(t, "status up after recovery", func() bool { return c.Status() == goapi.StatusUp })
	if rec := recvLog(t, c.Records()); rec.Message != "alive" {
		t.Fatalf("post-recovery record = %+v, want alive", rec)
	}
}

// TestLogsConsumerNon200IsAPIErrorNotSourceDown proves a reachable-but-rejecting
// go-api (e.g. 401 rejecting the operator token) flips the status down but keeps
// the typed distinction: an APIError, not source-down.
func TestLogsConsumerNon200IsAPIErrorNotSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("nope"))
	}))
	defer srv.Close()

	c, _ := newLogsConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLogsConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	eventually(t, "status down on 401", func() bool { return c.Status() == goapi.StatusDown })
	err := c.LastError()
	if goapi.IsSourceDown(err) {
		t.Fatalf("401 misclassified as source-down: %v", err)
	}
	var apiErr *goapi.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("LastError = %v (%T), want APIError 401", err, err)
	}
}

// TestLogsConsumerSkipsMalformedFrameKeepsStream proves one bad frame does not
// tear down the stream: a malformed data line is skipped and the next valid
// record still arrives (resilience of the reused decoder grammar).
func TestLogsConsumerSkipsMalformedFrameKeepsStream(t *testing.T) {
	when := time.Now().UTC()
	stub := &stubLogSSE{
		holdOpen: true,
		rawBody:  "data: {not valid json}\n\n",
		records:  []goapi.LogRecord{{Time: when, Level: "WARN", Message: "after garbage"}},
	}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newLogsConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLogsConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	if rec := recvLog(t, c.Records()); rec.Message != "after garbage" || rec.Level != "WARN" {
		t.Fatalf("record after malformed frame = %+v, want WARN/after garbage", rec)
	}
}

// TestLogsConsumerOversizedFrameReconnects proves the maxEventBytes cap the
// decoder inherits from sse.go holds on the log stream too: a frame that never
// terminates under the cap is surfaced as a dropped stream and the consumer
// reconnects rather than buffering without bound (resource-exhaustion); since
// every reconnect re-serves the same oversized frame, the flap reports
// connecting while it keeps retrying and only source-down once retries fail
// past the 10s reconnect grace.
func TestLogsConsumerOversizedFrameReconnects(t *testing.T) {
	huge := strings.Repeat("A", (1<<20)+16)
	stub := &stubLogSSE{rawBody: "data: " + huge} // no terminating blank line
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newLogsConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLogsConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	eventually(t, "oversized frame reports connecting while retrying", func() bool {
		return c.Status() == goapi.StatusConnecting
	})
	eventuallyWithin(t, "oversized frame surfaces source-down once retries fail past the 10s grace", 15*time.Second, func() bool {
		return c.Status() == goapi.StatusDown && goapi.IsSourceDown(c.LastError())
	})
}

// TestLogsConsumerRunOnce proves a LogsConsumer runs at most once: a second Run is
// rejected rather than racing a second producer onto the records channel.
func TestLogsConsumerRunOnce(t *testing.T) {
	srv := httptest.NewServer(&stubLogSSE{holdOpen: true})
	defer srv.Close()

	c, _ := newLogsConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLogsConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	eventually(t, "first Run took the started guard", func() bool { return c.Status() != goapi.StatusConnecting })
	if err := c.Run(context.Background()); err == nil {
		t.Fatal("second Run returned nil; expected an already-running error")
	}
}

// TestLogsConsumerShutdownClosesRecordsNoLeak proves clean shutdown: cancelling
// ctx returns Run with ctx.Err, closes the records channel, and leaks no goroutine
// per connection (the reconnect/stream watcher pattern).
func TestLogsConsumerShutdownClosesRecordsNoLeak(t *testing.T) {
	stub := &stubLogSSE{records: []goapi.LogRecord{{Time: time.Now().UTC(), Level: "INFO", Message: "tick"}}}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newLogsConsumer(t, srv.URL)
	before := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() { errCh <- c.Run(ctx) }()

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
	drainLogsUntilClosed(t, c.Records())
	eventually(t, "goroutines settle back near the pre-Run baseline", func() bool {
		runtime.GC()
		return runtime.NumGoroutine() <= before+3
	})
}

// TestNewLogsConsumerValidatesConfig rejects misconfiguration at construction.
func TestNewLogsConsumerValidatesConfig(t *testing.T) {
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
			if _, err := goapi.NewLogsConsumer(tc.baseURL, tc.tokens); err == nil {
				t.Fatalf("NewLogsConsumer(%q) returned nil error, want rejection", tc.baseURL)
			}
		})
	}
}

func drainLogsUntilClosed(t *testing.T, ch <-chan goapi.LogRecord) {
	t.Helper()
	timeout := time.After(2 * time.Second)
	for {
		select {
		case _, open := <-ch:
			if !open {
				return
			}
		case <-timeout:
			t.Fatal("records channel not closed after shutdown")
		}
	}
}
