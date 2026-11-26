package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/http/httptest"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

const testToken = "operator-jwt-token-value"

func TestHealthDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET", r.Method)
		}
		if r.URL.Path != "/health" {
			t.Errorf("stub got path %s, want /health", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	got, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health: unexpected error: %v", err)
	}
	if !got.OK() || got.Status != "ok" {
		t.Fatalf("Health = %+v, want status ok", got)
	}
}

func TestAttachesOperatorBearer(t *testing.T) {
	var gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminHealth(context.Background()); err != nil {
		t.Fatalf("AdminHealth: %v", err)
	}
	if want := "Bearer " + testToken; gotAuth != want {
		t.Fatalf("Authorization = %q, want %q", gotAuth, want)
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept = %q, want application/json", gotAccept)
	}
}

func TestUnreachableYieldsSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	c := newClient(t, url)
	_, err := c.Health(context.Background())
	if err == nil {
		t.Fatal("Health against a closed server returned nil error")
	}
	if !goapi.IsSourceDown(err) {
		t.Fatalf("error %v (%T) is not a SourceDownError", err, err)
	}
	var sd *goapi.SourceDownError
	if !errors.As(err, &sd) || sd.Err == nil {
		t.Fatalf("SourceDownError did not wrap the transport error: %+v", sd)
	}
}

func TestSendsCorrelationID(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-Correlation-ID")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).Health(context.Background()); err != nil {
		t.Fatalf("Health: %v", err)
	}
	if got == "" {
		t.Fatal("no X-Correlation-ID sent")
	}
	if len(got) > 64 {
		t.Fatalf("X-Correlation-ID %q exceeds go-api's 64-char cap; it would be rejected", got)
	}
	for _, c := range got {
		if (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') && c != '-' && c != '_' {
			t.Fatalf("X-Correlation-ID %q has a char go-api rejects: %q", got, c)
		}
	}
}

func TestAPIErrorRecordsEchoedCorrelationID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-Correlation-ID", "srvecho01")
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).Health(context.Background())
	var apiErr *goapi.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v (%T) is not an APIError", err, err)
	}
	if apiErr.CorrID != "srvecho01" {
		t.Fatalf("APIError.CorrID = %q, want srvecho01 (echoed off the response)", apiErr.CorrID)
	}
}

func TestSourceDownRecordsOutboundCorrelationID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := newClient(t, url).Health(context.Background())
	var sd *goapi.SourceDownError
	if !errors.As(err, &sd) {
		t.Fatalf("error %v (%T) is not a SourceDownError", err, err)
	}
	if sd.CorrID == "" {
		t.Fatal("SourceDownError.CorrID is empty; the outbound correlation id was not recorded")
	}
}

func TestTimeoutIsSourceDown(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	c, err := goapi.New(srv.URL, goapi.StaticTokenSource(testToken), goapi.WithTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.Health(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("timeout error = %v, want source-down", err)
	}
}

func TestNon2xxYieldsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"reason":"expired"}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).Health(context.Background())
	if goapi.IsSourceDown(err) {
		t.Fatalf("401 misclassified as source-down: %v", err)
	}
	var apiErr *goapi.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v (%T) is not an APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("APIError.StatusCode = %d, want 401", apiErr.StatusCode)
	}
}

func TestMalformedBodyIsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).Health(context.Background()); err == nil {
		t.Fatal("malformed body returned nil error")
	}
}

func TestOversizedBodyIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"`))
		_, _ = w.Write([]byte(strings.Repeat("a", 2<<20)))
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).Health(context.Background()); err == nil {
		t.Fatal("oversized body returned nil error; read was not bounded")
	}
}

func TestTokenSourceErrorFailsClosed(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit = true
	}))
	defer srv.Close()

	c, err := goapi.New(srv.URL, goapi.StaticTokenSource(""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.AdminHealth(context.Background()); err == nil {
		t.Fatal("empty token source returned nil error")
	}
	if hit {
		t.Fatal("a request was sent despite no token being available")
	}
}

func TestNewValidatesConfig(t *testing.T) {
	cases := []struct {
		name    string
		baseURL string
		tokens  goapi.TokenSource
	}{
		{"empty base url", "", goapi.StaticTokenSource(testToken)},
		{"no scheme", "api.altune.app", goapi.StaticTokenSource(testToken)},
		{"unsupported scheme", "ftp://api.altune.app", goapi.StaticTokenSource(testToken)},
		{"missing host", "https://", goapi.StaticTokenSource(testToken)},
		{"nil token source", "https://api.altune.app", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := goapi.New(tc.baseURL, tc.tokens); err == nil {
				t.Fatalf("New(%q) returned nil error, want rejection", tc.baseURL)
			}
		})
	}
}

func newClient(t *testing.T, baseURL string) *goapi.Client {
	t.Helper()
	c, err := goapi.New(baseURL, goapi.StaticTokenSource(testToken))
	if err != nil {
		t.Fatalf("New(%q): %v", baseURL, err)
	}
	return c
}

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
		<-r.Context().Done()
	}
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
	if ev := recv(t, c.Events()); ev.Type != "alive" {
		t.Fatalf("post-recovery event = %+v, want alive", ev)
	}
}

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

func TestConsumerShutdownClosesEventsNoLeak(t *testing.T) {
	stub := &stubSSE{events: []goapi.Event{{Type: "tick", Timestamp: time.Now().UTC()}}}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newConsumer(t, srv.URL)
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
	drainUntilClosed(t, c.Events())
	eventually(t, "goroutines settle back near the pre-Run baseline", func() bool {
		runtime.GC()
		return runtime.NumGoroutine() <= before+3
	})
}

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

const providerUsageBody = `{
	"auth": {"token_rejections_total": 3},
	"latency": {"routes": {}},
	"providers": {
		"deezer":  {"ok": 120, "quota": 4, "error": 2},
		"spotify": {"ok": 30, "quota": 0, "error": 1},
		"other":   {"ok": 0, "quota": 0, "error": 0}
	}
}`

func TestAdminProviderUsageDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET", r.Method)
		}
		if r.URL.Path != "/observe/metrics/live" {
			t.Errorf("stub got path %s, want /observe/metrics/live", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(providerUsageBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminProviderUsage(context.Background())
	if err != nil {
		t.Fatalf("AdminProviderUsage: unexpected error: %v", err)
	}
	deezer, ok := got["deezer"]
	if !ok {
		t.Fatalf("deezer not decoded; got providers %+v", got)
	}
	if deezer.OK != 120 || deezer.Quota != 4 || deezer.Error != 2 {
		t.Fatalf("deezer = %+v, want ok=120 quota=4 error=2", deezer)
	}
	if got := deezer.Total(); got != 126 {
		t.Fatalf("deezer.Total() = %d, want 126", got)
	}
	if spotify := got["spotify"]; spotify.OK != 30 || spotify.Error != 1 {
		t.Fatalf("spotify = %+v, want ok=30 error=1", spotify)
	}
	if other := got["other"]; other.Total() != 0 {
		t.Fatalf("other.Total() = %d, want 0", other.Total())
	}
}

func TestAdminProviderUsageAttachesOperatorBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"providers":{}}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminProviderUsage(context.Background()); err != nil {
		t.Fatalf("AdminProviderUsage: %v", err)
	}
	if want := "Bearer " + testToken; gotAuth != want {
		t.Fatalf("Authorization = %q, want %q", gotAuth, want)
	}
}

func TestAdminProviderUsageUnreachableYieldsSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := newClient(t, url).AdminProviderUsage(context.Background())
	if !goapi.IsSourceDown(err) {
		t.Fatalf("error %v (%T) is not a SourceDownError", err, err)
	}
	var sd *goapi.SourceDownError
	if !errors.As(err, &sd) || sd.Err == nil {
		t.Fatalf("SourceDownError did not wrap the transport error: %+v", sd)
	}
}

func TestAdminProviderUsageForbiddenYieldsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"operator access required"}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).AdminProviderUsage(context.Background())
	if goapi.IsSourceDown(err) {
		t.Fatalf("403 misclassified as source-down: %v", err)
	}
	var apiErr *goapi.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v (%T) is not an APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("APIError.StatusCode = %d, want 403", apiErr.StatusCode)
	}
}

func TestProviderOutcomesTotalSaturatesOnOverflow(t *testing.T) {
	for _, tc := range []struct {
		name string
		o    goapi.ProviderOutcomes
		want int64
	}{
		{"benign sum", goapi.ProviderOutcomes{OK: 120, Quota: 4, Error: 2}, 126},
		{"all zero stays zero", goapi.ProviderOutcomes{}, 0},
		{"two at ceiling saturate", goapi.ProviderOutcomes{OK: math.MaxInt64, Quota: math.MaxInt64}, math.MaxInt64},
		{"all three at ceiling saturate", goapi.ProviderOutcomes{OK: math.MaxInt64, Quota: math.MaxInt64, Error: math.MaxInt64}, math.MaxInt64},
		{"ceiling plus one saturates", goapi.ProviderOutcomes{OK: math.MaxInt64, Error: 1}, math.MaxInt64},
	} {
		if got := tc.o.Total(); got != tc.want {
			t.Errorf("%s: Total() = %d, want %d", tc.name, got, tc.want)
		}
		if got := tc.o.Total(); got < 0 {
			t.Errorf("%s: Total() wrapped negative to %d", tc.name, got)
		}
	}
}

const adminEvalBody = `{
	"enabled": true,
	"paused": false,
	"state": "ok",
	"score": 0.81,
	"baseline": 0.75,
	"last_run": "2026-09-15T10:04:05Z",
	"queries": [
		{"query": "miles davis", "expect": "kind of blue", "passed": true, "position": 1},
		{"query": "radiohead", "expect": "ok computer", "passed": false, "position": 7}
	]
}`

const adminAcquisitionBody = `{
	"in_flight": 2,
	"succeeded": 19,
	"failed": 1,
	"rejected": 3,
	"queue_depth": 4,
	"queue_capacity": 64
}`

func TestAdminEvalDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET", r.Method)
		}
		if r.URL.Path != "/observe/eval" {
			t.Errorf("stub got path %s, want /observe/eval", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(adminEvalBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminEval(context.Background())
	if err != nil {
		t.Fatalf("AdminEval: unexpected error: %v", err)
	}
	if !got.Scored() {
		t.Fatal("Scored() = false for a scored eval status")
	}
	if got.Score == nil || *got.Score != 0.81 || got.Baseline == nil || *got.Baseline != 0.75 {
		t.Fatalf("score/baseline = %+v, want 0.81/0.75", got)
	}
	if got.State != "ok" || !got.Enabled || got.Paused {
		t.Fatalf("meta = %+v, want state=ok enabled paused=false", got)
	}
	if len(got.Queries) != 2 || got.Queries[0].Query != "miles davis" || got.Queries[1].Passed {
		t.Fatalf("queries decoded wrong: %+v", got.Queries)
	}
	want := time.Date(2026, 9, 15, 10, 4, 5, 0, time.UTC)
	if got.LastRun == nil || !got.LastRun.Equal(want) {
		t.Fatalf("LastRun = %v, want %v", got.LastRun, want)
	}
}

func TestAdminEvalNoDataLeavesScoreNil(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"enabled":true,"state":"no_data"}`))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminEval(context.Background())
	if err != nil {
		t.Fatalf("AdminEval: %v", err)
	}
	if got.Scored() || got.Score != nil {
		t.Fatalf("Score should be nil for no_data, got %+v", got)
	}
}

func TestAdminAcquisitionDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/observe/acquisition" {
			t.Errorf("stub got path %s, want /observe/acquisition", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(adminAcquisitionBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminAcquisition(context.Background())
	if err != nil {
		t.Fatalf("AdminAcquisition: unexpected error: %v", err)
	}
	if got.Succeeded != 19 || got.Failed != 1 || got.Rejected != 3 {
		t.Fatalf("counters = %+v, want succeeded=19 failed=1 rejected=3", got)
	}
	if got.InFlight != 2 || got.QueueDepth != 4 || got.QueueCapacity != 64 {
		t.Fatalf("gauges = %+v, want in_flight=2 depth=4 cap=64", got)
	}
	rate, ok := got.SuccessRateSince(goapi.AcquisitionStatus{})
	if !ok || rate != 19.0/20.0 {
		t.Fatalf("SuccessRateSince(zero) = %v (ok=%v), want 0.95", rate, ok)
	}
}

func TestAcquisitionWindowedRateSeesRecentSpike(t *testing.T) {
	base := goapi.AcquisitionStatus{Succeeded: 990, Failed: 10}
	now := goapi.AcquisitionStatus{Succeeded: 990, Failed: 110}

	rate, ok := now.SuccessRateSince(base)
	if !ok {
		t.Fatal("windowed rate undefined despite 100 recent completions")
	}
	if rate != 0 {
		t.Fatalf("windowed rate = %v, want 0 — the recent window is all failures", rate)
	}
}

func TestAcquisitionWindowedRateUndefinedWithNoRecentCompletions(t *testing.T) {
	base := goapi.AcquisitionStatus{Succeeded: 42, Failed: 7}
	if rate, ok := base.SuccessRateSince(base); ok || rate != 0 {
		t.Fatalf("windowed rate over an idle window = %v (ok=%v), want 0 undefined", rate, ok)
	}
}

func TestAcquisitionWindowedRateUndefinedAcrossCounterReset(t *testing.T) {
	base := goapi.AcquisitionStatus{Succeeded: 990, Failed: 10}
	afterRestart := goapi.AcquisitionStatus{Succeeded: 3, Failed: 1}
	if rate, ok := afterRestart.SuccessRateSince(base); ok || rate != 0 {
		t.Fatalf("windowed rate across a reset = %v (ok=%v), want 0 undefined", rate, ok)
	}
}

func TestAcquisitionWindowedRateSurvivesCounterOverflow(t *testing.T) {
	const maxU64 = ^uint64(0)
	for _, tc := range []struct {
		name             string
		succeeded, faild uint64
		wantOK           bool
		wantRate         float64
	}{
		{"exact wrap to zero", 1 << 63, 1 << 63, true, 0.5},
		{"partial wrap", maxU64, 5, true, float64(maxU64) / (float64(maxU64) + 5)},
		{"both at ceiling", maxU64, maxU64, true, 0.5},
	} {
		a := goapi.AcquisitionStatus{Succeeded: tc.succeeded, Failed: tc.faild}
		rate, ok := a.SuccessRateSince(goapi.AcquisitionStatus{})
		if ok != tc.wantOK {
			t.Fatalf("%s: ok = %v, want %v", tc.name, ok, tc.wantOK)
		}
		if rate != tc.wantRate {
			t.Fatalf("%s: rate = %v, want %v", tc.name, rate, tc.wantRate)
		}
		if rate < 0 || rate > 1 {
			t.Fatalf("%s: rate %v escaped [0,1]", tc.name, rate)
		}
	}
}

func TestEvalAgeReportsElapsedSinceLastRun(t *testing.T) {
	ran := time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)
	now := ran.Add(3 * time.Hour)
	e := goapi.EvalStatus{LastRun: &ran}

	age, known := e.Age(now)
	if !known {
		t.Fatal("Age reported unknown for a scored meter with a last_run")
	}
	if age != 3*time.Hour {
		t.Fatalf("Age = %v, want 3h", age)
	}
}

func TestEvalAgeUnknownWhenNeverRun(t *testing.T) {
	if _, known := (goapi.EvalStatus{}).Age(time.Now()); known {
		t.Fatal("Age reported known for a meter that never ran")
	}
}

func TestEvalStaleByAgePastThreshold(t *testing.T) {
	ran := time.Date(2026, 9, 13, 6, 0, 0, 0, time.UTC)
	now := ran.Add(5 * 24 * time.Hour)
	e := goapi.EvalStatus{LastRun: &ran}

	if !e.StaleByAge(now, 12*time.Hour) {
		t.Fatal("a 5-day-old score was not flagged stale past a 12h threshold")
	}
}

func TestEvalFreshWithinThresholdNotStale(t *testing.T) {
	ran := time.Date(2026, 9, 18, 6, 0, 0, 0, time.UTC)
	now := ran.Add(2 * time.Hour)
	e := goapi.EvalStatus{LastRun: &ran}

	if e.StaleByAge(now, 12*time.Hour) {
		t.Fatal("a 2h-old score was wrongly flagged stale past a 12h threshold")
	}
}

func TestEvalNeverRunIsNotStale(t *testing.T) {
	if (goapi.EvalStatus{}).StaleByAge(time.Now(), 12*time.Hour) {
		t.Fatal("a never-run meter was wrongly flagged stale by age")
	}
}

func TestAdminEvalUnreachableYieldsSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	if _, err := newClient(t, url).AdminEval(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("AdminEval against a closed server: err = %v, want source-down", err)
	}
	if _, err := newClient(t, url).AdminAcquisition(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("AdminAcquisition against a closed server: err = %v, want source-down", err)
	}
}

func TestAdminEvalForbiddenYieldsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"operator access required"}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).AdminEval(context.Background())
	if goapi.IsSourceDown(err) {
		t.Fatalf("403 misclassified as source-down: %v", err)
	}
	var apiErr *goapi.APIError
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("error %v (%T) is not a 403 APIError", err, err)
	}
}

func TestAdminEvalAttachesOperatorBearer(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*goapi.Client) error
	}{
		{"eval", func(c *goapi.Client) error { _, e := c.AdminEval(context.Background()); return e }},
		{"acquisition", func(c *goapi.Client) error { _, e := c.AdminAcquisition(context.Background()); return e }},
	} {
		var gotAuth string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			gotAuth = r.Header.Get("Authorization")
			_, _ = w.Write([]byte(`{}`))
		}))
		if err := tc.call(newClient(t, srv.URL)); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		srv.Close()
		if want := "Bearer " + testToken; gotAuth != want {
			t.Fatalf("%s Authorization = %q, want %q", tc.name, gotAuth, want)
		}
	}
}

func TestAdminEvalMalformedBodyIsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminEval(context.Background()); err == nil {
		t.Fatal("malformed eval body returned nil error")
	}
}

func TestHealthProbesPublicEvenWhenTokenSourceAlwaysErrors(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hit = true
		if auth := r.Header.Get("Authorization"); auth != "" {
			t.Errorf("Authorization = %q, want none on the public probe", auth)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer srv.Close()

	c, err := goapi.New(srv.URL, goapi.StaticTokenSource(""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got, err := c.Health(context.Background())
	if err != nil {
		t.Fatalf("Health with an always-erroring token source: %v", err)
	}
	if !hit {
		t.Fatal("Health never reached the server despite the endpoint being public")
	}
	if !got.OK() {
		t.Fatalf("Health = %+v, want ok", got)
	}
}

func TestHealthDegradedBodyIsReadableNotAnAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"status":"degraded"}`))
	}))
	defer srv.Close()

	c := newClient(t, srv.URL)
	got, err := c.Health(context.Background())
	if err != nil {
		var apiErr *goapi.APIError
		if errors.As(err, &apiErr) {
			t.Fatalf("Health: %v, want the 503 degraded body decoded, not an APIError", err)
		}
		t.Fatalf("Health: unexpected error: %v", err)
	}
	if got.OK() {
		t.Fatalf("Health = %+v, want not ok", got)
	}
	if !got.Degraded() {
		t.Fatalf("Health = %+v, want degraded", got)
	}
}

const adminHealthBody = `{
	"db": "ok",
	"redis": "ok",
	"auth": "ok",
	"detail": {
		"db_latency_ms": 3,
		"redis_latency_ms": 1,
		"auth_latency_ms": 12,
		"checked_at": "2026-09-15T10:04:05Z"
	},
	"goroutines": 42,
	"heap_mb": 17
}`

func TestAdminHealthDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET", r.Method)
		}
		if r.URL.Path != "/observe/health" {
			t.Errorf("stub got path %s, want /observe/health", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(adminHealthBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminHealth(context.Background())
	if err != nil {
		t.Fatalf("AdminHealth: unexpected error: %v", err)
	}
	if got.DB != "ok" || got.Redis != "ok" || got.Auth != "ok" {
		t.Fatalf("dependency pills = %+v, want all ok", got)
	}
	if got.Detail.AuthLatencyMs != 12 || got.Goroutines != 42 || got.HeapMB != 17 {
		t.Fatalf("decoded detail/gauges = %+v, want auth=12 goroutines=42 heap=17", got)
	}
	want := time.Date(2026, 9, 15, 10, 4, 5, 0, time.UTC)
	if !got.Detail.CheckedAt.Equal(want) {
		t.Fatalf("CheckedAt = %v, want %v", got.Detail.CheckedAt, want)
	}
	if !got.Healthy() {
		t.Fatal("Healthy() = false for an all-ok snapshot")
	}
}

func TestAdminHealthReportsDownDependency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"db":"ok","redis":"down","auth":"ok","detail":{"redis_error":"dial tcp: connection refused"}}`))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminHealth(context.Background())
	if err != nil {
		t.Fatalf("AdminHealth: %v", err)
	}
	if got.Healthy() {
		t.Fatal("Healthy() = true despite redis down")
	}
	if got.Detail.RedisError == "" {
		t.Fatal("RedisError empty; the degraded reason did not decode")
	}
}

func TestAdminHealthAttachesOperatorBearer(t *testing.T) {
	var gotAuth, gotAccept string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		gotAccept = r.Header.Get("Accept")
		_, _ = w.Write([]byte(`{"db":"ok","redis":"ok","auth":"ok"}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminHealth(context.Background()); err != nil {
		t.Fatalf("AdminHealth: %v", err)
	}
	if want := "Bearer " + testToken; gotAuth != want {
		t.Fatalf("Authorization = %q, want %q", gotAuth, want)
	}
	if gotAccept != "application/json" {
		t.Fatalf("Accept = %q, want application/json", gotAccept)
	}
}

func TestAdminHealthUnreachableYieldsSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := newClient(t, url).AdminHealth(context.Background())
	if err == nil {
		t.Fatal("AdminHealth against a closed server returned nil error")
	}
	if !goapi.IsSourceDown(err) {
		t.Fatalf("error %v (%T) is not a SourceDownError", err, err)
	}
	var sd *goapi.SourceDownError
	if !errors.As(err, &sd) || sd.Err == nil {
		t.Fatalf("SourceDownError did not wrap the transport error: %+v", sd)
	}
}

func TestAdminHealthForbiddenYieldsAPIError(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"error":"operator access required"}`))
		}))

		_, err := newClient(t, srv.URL).AdminHealth(context.Background())
		srv.Close()
		if goapi.IsSourceDown(err) {
			t.Fatalf("status %d misclassified as source-down: %v", status, err)
		}
		var apiErr *goapi.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("error %v (%T) is not an APIError", err, err)
		}
		if apiErr.StatusCode != status {
			t.Fatalf("APIError.StatusCode = %d, want %d", apiErr.StatusCode, status)
		}
	}
}

func TestAdminHealthTimeoutIsSourceDown(t *testing.T) {
	block := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		<-block
	}))
	defer srv.Close()
	defer close(block)

	c, err := goapi.New(srv.URL, goapi.StaticTokenSource(testToken), goapi.WithTimeout(50*time.Millisecond))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.AdminHealth(context.Background()); !goapi.IsSourceDown(err) {
		t.Fatalf("timeout error = %v, want source-down", err)
	}
}

func TestAdminHealthMalformedBodyIsDecodeError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`not json at all`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminHealth(context.Background()); err == nil {
		t.Fatal("malformed body returned nil error")
	}
}

func TestAdminHealthOversizedBodyIsBounded(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"db":"`))
		_, _ = w.Write([]byte(strings.Repeat("a", 2<<20)))
		_, _ = w.Write([]byte(`"}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminHealth(context.Background()); err == nil {
		t.Fatal("oversized body returned nil error; read was not bounded")
	}
}

func TestAdminHealthTokenSourceErrorFailsClosed(t *testing.T) {
	var hit bool
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		hit = true
	}))
	defer srv.Close()

	c, err := goapi.New(srv.URL, goapi.StaticTokenSource(""))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := c.AdminHealth(context.Background()); err == nil {
		t.Fatal("empty token source returned nil error")
	}
	if hit {
		t.Fatal("a request was sent despite no token being available")
	}
}

func TestAdminHealthDegradedBodyDecodesLive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"db":"ok","redis":"down","auth":"ok","detail":{"redis_error":"dial tcp: connection refused"}}`))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminHealth(context.Background())
	if err != nil {
		t.Fatalf("AdminHealth: unexpected error on 503 body: %v", err)
	}
	if got.Healthy() {
		t.Fatal("Healthy() = true despite redis down")
	}
	if got.DB != "ok" || got.Redis != "down" || got.Auth != "ok" {
		t.Fatalf("dependency pills = %+v, want db=ok redis=down auth=ok", got)
	}
	if got.Detail.RedisError == "" {
		t.Fatal("RedisError empty; the 503 detail did not decode")
	}
}

func TestAdminHealthNon503NonOKStillAPIError(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusInternalServerError} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(`{"db":"ok","redis":"down","auth":"ok"}`))
		}))

		_, err := newClient(t, srv.URL).AdminHealth(context.Background())
		srv.Close()

		var apiErr *goapi.APIError
		if !errors.As(err, &apiErr) {
			t.Fatalf("status %d: error %v (%T) is not an APIError", status, err, err)
		}
		if apiErr.StatusCode != status {
			t.Fatalf("APIError.StatusCode = %d, want %d", apiErr.StatusCode, status)
		}
		if goapi.Classify(err) == goapi.ReasonDegraded {
			t.Fatalf("status %d misclassified as degraded", status)
		}
	}
}

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

func TestLogsConsumerOversizedFrameReconnects(t *testing.T) {
	huge := strings.Repeat("A", (1<<20)+16)
	stub := &stubLogSSE{rawBody: "data: " + huge}
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

func TestLogsConsumerLastErrorClearsOnRecovery(t *testing.T) {
	stub := &stubLogSSE{holdOpen: true, records: []goapi.LogRecord{{Time: time.Now().UTC(), Level: "INFO", Message: "alive"}}}
	srv := httptest.NewServer(stub)
	defer srv.Close()

	c, _ := newLogsConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLogsConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	recvLog(t, c.Records())
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
	recvLog(t, c.Records())
	if err := c.LastError(); err != nil {
		t.Fatalf("LastError after recovery = %v, want nil", err)
	}
}

const metricsLiveBody = `{
	"auth": {"token_rejections_total": 3},
	"catalog": {"lookups_total": 10},
	"latency": {
		"routes": {
			"/v1/tracks/{trackId}": {
				"count": 100,
				"sum_ms": 750,
				"status": {"2xx": 90, "4xx": 6, "5xx": 4},
				"buckets": [
					{"le_ms": "1", "count": 0},
					{"le_ms": "5", "count": 0},
					{"le_ms": "10", "count": 100},
					{"le_ms": "+Inf", "count": 0}
				]
			}
		}
	}
}`

func TestAdminMetricsLiveDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET", r.Method)
		}
		if r.URL.Path != "/observe/metrics/live" {
			t.Errorf("stub got path %s, want /observe/metrics/live", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(metricsLiveBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminMetricsLive(context.Background())
	if err != nil {
		t.Fatalf("AdminMetricsLive: unexpected error: %v", err)
	}
	route, ok := got.Latency.Routes["/v1/tracks/{trackId}"]
	if !ok {
		t.Fatalf("route not decoded; got routes %+v", got.Latency.Routes)
	}
	if route.Count != 100 || route.SumMs != 750 {
		t.Fatalf("route count/sum = %d/%d, want 100/750", route.Count, route.SumMs)
	}
	if len(route.Buckets) != 4 {
		t.Fatalf("decoded %d buckets, want 4", len(route.Buckets))
	}
	if route.Buckets[2].LeMs != "10" || route.Buckets[2].Count != 100 {
		t.Fatalf("bucket[2] = %+v, want le_ms=10 count=100", route.Buckets[2])
	}
	if route.Buckets[3].LeMs != "+Inf" {
		t.Fatalf("final bucket label = %q, want +Inf", route.Buckets[3].LeMs)
	}
}

func TestAdminMetricsLiveDecodesStatusClasses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(metricsLiveBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminMetricsLive(context.Background())
	if err != nil {
		t.Fatalf("AdminMetricsLive: unexpected error: %v", err)
	}

	status := got.Latency.Routes["/v1/tracks/{trackId}"].Status
	if status.Count2xx != 90 || status.Count4xx != 6 || status.Count5xx != 4 {
		t.Fatalf("status classes = %+v, want 2xx=90 4xx=6 5xx=4", status)
	}
}

func TestAdminMetricsLiveAttachesOperatorBearer(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_, _ = w.Write([]byte(`{"latency":{"routes":{}}}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminMetricsLive(context.Background()); err != nil {
		t.Fatalf("AdminMetricsLive: %v", err)
	}
	if want := "Bearer " + testToken; gotAuth != want {
		t.Fatalf("Authorization = %q, want %q", gotAuth, want)
	}
}

func TestAdminMetricsLiveUnreachableYieldsSourceDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()

	_, err := newClient(t, url).AdminMetricsLive(context.Background())
	if !goapi.IsSourceDown(err) {
		t.Fatalf("error %v (%T) is not a SourceDownError", err, err)
	}
	var sd *goapi.SourceDownError
	if !errors.As(err, &sd) || sd.Err == nil {
		t.Fatalf("SourceDownError did not wrap the transport error: %+v", sd)
	}
}

func TestAdminMetricsLiveForbiddenYieldsAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"operator access required"}`))
	}))
	defer srv.Close()

	_, err := newClient(t, srv.URL).AdminMetricsLive(context.Background())
	if goapi.IsSourceDown(err) {
		t.Fatalf("403 misclassified as source-down: %v", err)
	}
	var apiErr *goapi.APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error %v (%T) is not an APIError", err, err)
	}
	if apiErr.StatusCode != http.StatusForbidden {
		t.Fatalf("APIError.StatusCode = %d, want 403", apiErr.StatusCode)
	}
}

var readOnlyMethods = map[string]bool{
	"Health":                  true,
	"AdminHealth":             true,
	"AdminEval":               true,
	"AdminAcquisition":        true,
	"AdminMetricsLive":        true,
	"AdminProviderUsage":      true,
	"AdminDiscographyQuality": true,
}

var mutatingVerbs = []string{
	"post", "put", "patch", "delete", "create", "update", "write", "mutate",
	"command", "send", "trigger", "remove", "reacquire", "retry", "enqueue",
	"publish", "set", "insert", "upsert", "modify", "kill", "restart", "flip",
	"do",
}

func TestClientExposesOnlyReads(t *testing.T) {
	typ := reflect.TypeOf(&goapi.Client{})
	if typ.NumMethod() == 0 {
		t.Fatal("client exposes no methods; expected at least one read")
	}
	for i := 0; i < typ.NumMethod(); i++ {
		name := typ.Method(i).Name
		lower := strings.ToLower(name)
		for _, verb := range mutatingVerbs {
			if strings.Contains(lower, verb) {
				t.Errorf("client exposes mutating method %q (matched %q): observe-only violated", name, verb)
			}
		}
		if !readOnlyMethods[name] {
			t.Errorf("client exposes unlisted method %q; if it is a read, add it to readOnlyMethods", name)
		}
	}
}

const adminDiscographyQualityBody = `{
	"window_days": 30,
	"group_by": "artist",
	"suspect_rate": 0.375,
	"last_sample_at": "2026-09-16T08:30:00Z",
	"cases": [
		{
			"artist": "Radiohead",
			"artist_ref": "spotify:4Z8W4fKeB5YxbusRsdQVPb",
			"releases": 42,
			"single_provider": 9,
			"single_provider_no_id": 4,
			"provider_counts": {"spotify": 40, "musicbrainz": 12},
			"last_seen": "2026-09-15T12:00:00Z"
		}
	]
}`

func TestAdminDiscographyQualityDecodesStubbedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("stub got method %s, want GET (the read must never mutate)", r.Method)
		}
		if r.URL.Path != "/observe/quality/discography" {
			t.Errorf("stub got path %s, want /observe/quality/discography", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(adminDiscographyQualityBody))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminDiscographyQuality(context.Background())
	if err != nil {
		t.Fatalf("AdminDiscographyQuality: unexpected error: %v", err)
	}
	if got.WindowDays != 30 || got.GroupBy != "artist" {
		t.Fatalf("window/group = %d/%q, want 30/artist", got.WindowDays, got.GroupBy)
	}
	if len(got.Cases) != 1 {
		t.Fatalf("cases = %d, want 1", len(got.Cases))
	}
	c := got.Cases[0]
	if c.Artist != "Radiohead" || c.ArtistRef != "spotify:4Z8W4fKeB5YxbusRsdQVPb" {
		t.Errorf("artist = %q ref = %q", c.Artist, c.ArtistRef)
	}
	if c.Releases != 42 || c.SingleProvider != 9 {
		t.Errorf("releases/single = %d/%d, want 42/9", c.Releases, c.SingleProvider)
	}
	if c.SingleProviderNoID != 4 {
		t.Errorf("single_provider_no_id = %d, want 4 (the id-backing evidence)", c.SingleProviderNoID)
	}
	if c.ProviderCounts["spotify"] != 40 || c.ProviderCounts["musicbrainz"] != 12 {
		t.Errorf("provider_counts = %v", c.ProviderCounts)
	}
	want := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	if !c.LastSeen.Equal(want) {
		t.Errorf("last_seen = %v, want %v", c.LastSeen, want)
	}
	if got.SuspectRate != 0.375 {
		t.Errorf("suspect_rate = %v, want 0.375 (the served windowed headline)", got.SuspectRate)
	}
	wantSample := time.Date(2026, 9, 16, 8, 30, 0, 0, time.UTC)
	if !got.LastSampleAt.Equal(wantSample) {
		t.Errorf("last_sample_at = %v, want %v", got.LastSampleAt, wantSample)
	}
}

func TestAdminDiscographyQualityToleratesVersionSkew(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"window_days":7,"group_by":"artist","trend":[1,2,3],"cases":[{"artist_ref":"deezer:1","releases":3,"single_provider":1,"provider_counts":{"deezer":3},"future_field":"x"}]}`))
	}))
	defer srv.Close()

	got, err := newClient(t, srv.URL).AdminDiscographyQuality(context.Background())
	if err != nil {
		t.Fatalf("AdminDiscographyQuality with extra fields: %v", err)
	}
	if got.WindowDays != 7 || len(got.Cases) != 1 || got.Cases[0].ArtistRef != "deezer:1" {
		t.Fatalf("decoded = %+v, want window 7 and one deezer case", got)
	}
}

func TestAdminDiscographyQualityRejectsNonOperator(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"operator access required"}`))
	}))
	defer srv.Close()

	if _, err := newClient(t, srv.URL).AdminDiscographyQuality(context.Background()); err == nil {
		t.Fatal("AdminDiscographyQuality: want an error on a 403, got nil")
	}
}
