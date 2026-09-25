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
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func serveSSEWithTokenExpiry(t *testing.T, h *sseHandler, uid shared.UserId, expiresAt time.Time) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := auth.ContextWithTokenExpiry(auth.ContextWithUserID(r.Context(), uid), expiresAt)
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
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	h := newSSEHandler(bus, 0)
	h.heartbeat = 20 * time.Millisecond
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

func TestSSEHandler_StreamEndingAtTokenExpiryFreesUserSlot(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	h := newSSEHandler(bus, 0)
	h.limiter = newConnLimiter(1, 0)
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

func TestSSEHandler_StreamWithoutTokenExpiryStaysOpen(t *testing.T) {
	bus := events.NewInProcessBus()
	uid := shared.NewUserId(uuid.New())
	srv := newTestSSEServer(t, bus, uid, 20*time.Millisecond)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("GET: %v", err)
	}
	defer resp.Body.Close()
	br := bufio.NewReader(resp.Body)
	readUntil(t, br, func(l string) bool { return strings.HasPrefix(l, ":ok") })

	if err := readToEOF(t, br, 300*time.Millisecond); err == nil {
		t.Fatal("stream with no token expiry on its context ended on its own")
	}
}
