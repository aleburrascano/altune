package server_test

import (
	"altune/go-api/internal/app/server"
	"altune/go-api/internal/shared/timingtest"
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

func TestNew_AnInFlightRequestFinishesWithALiveContextWhileTheServerDrains(t *testing.T) {
	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		result <- r.Context().Err()
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := server.New(listener.Addr().String(), slow)
	go func() { _ = srv.Serve(listener) }()
	go func() {
		if resp, err := http.Get("http://" + listener.Addr().String()); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started

	drained := make(chan error, 1)
	drainCtx, cancelDrain := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancelDrain()
	go func() { drained <- srv.Shutdown(drainCtx) }()
	timingtest.Until(t, 2*time.Second, "Shutdown to stop accepting connections", func() bool {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			return true
		}
		_ = conn.Close()
		return false
	})
	close(release)

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("in-flight request context cancelled while the server drained: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish")
	}
	if err := <-drained; err != nil {
		t.Errorf("Shutdown = %v, want the drain to finish once the request did", err)
	}
}

func TestNew_ServesTheHandlerAtTheAddressWithTheFixedTimeouts(t *testing.T) {
	h := http.NotFoundHandler()

	srv := server.New("127.0.0.1:8000", h)

	if srv.Addr != "127.0.0.1:8000" || srv.Handler == nil {
		t.Errorf("server = %q %v, want the given address and handler", srv.Addr, srv.Handler)
	}
	if srv.ReadHeaderTimeout != 10*time.Second || srv.ReadTimeout != 30*time.Second || srv.IdleTimeout != 120*time.Second {
		t.Errorf("timeouts = header %v, read %v, idle %v, want 10s, 30s, 120s", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.IdleTimeout)
	}
}
