package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/events"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
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
	h := newSSEHandler(bus)
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
	h := newSSEHandler(bus)
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
	h := newSSEHandler(bus)
	h.limiter = newConnLimiter(2)
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
	l := newConnLimiter(2)
	if !l.acquire("u") {
		t.Fatal("first acquire should succeed")
	}
	if !l.acquire("u") {
		t.Fatal("second acquire should succeed")
	}
	if l.acquire("u") {
		t.Fatal("third acquire should be rejected at the cap")
	}
	l.release("u")
	if !l.acquire("u") {
		t.Fatal("acquire should succeed after a release frees a slot")
	}
	if !l.acquire("v") {
		t.Fatal("a different user must have an independent budget")
	}
}
