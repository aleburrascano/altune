package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"bufio"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

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
		bus.Publish(uid, "still_live", map[string]any{"i": i})
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
