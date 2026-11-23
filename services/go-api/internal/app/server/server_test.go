package server_test

import (
	"altune/go-api/internal/app/server"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNew_AnInFlightRequestOutlivesTheLifecycleCancel(t *testing.T) {
	lifecycle, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		result <- r.Context().Err()
	})
	srv := httptest.NewUnstartedServer(slow)
	srv.Config.BaseContext = server.New(lifecycle, "", slow).BaseContext
	srv.Start()
	defer srv.Close()

	go func() {
		if resp, err := http.Get(srv.URL); err == nil {
			_ = resp.Body.Close()
		}
	}()
	<-started
	cancel()
	time.Sleep(20 * time.Millisecond)
	close(release)

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("in-flight request context cancelled by lifecycle cancel: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not finish")
	}
}

func TestNew_ServesTheHandlerAtTheAddressWithTheFixedTimeouts(t *testing.T) {
	h := http.NotFoundHandler()

	srv := server.New(context.Background(), "127.0.0.1:8000", h)

	if srv.Addr != "127.0.0.1:8000" || srv.Handler == nil {
		t.Errorf("server = %q %v, want the given address and handler", srv.Addr, srv.Handler)
	}
	if srv.ReadHeaderTimeout != 10*time.Second || srv.ReadTimeout != 30*time.Second || srv.IdleTimeout != 120*time.Second {
		t.Errorf("timeouts = header %v, read %v, idle %v, want 10s, 30s, 120s", srv.ReadHeaderTimeout, srv.ReadTimeout, srv.IdleTimeout)
	}
}
