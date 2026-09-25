package app

import (
	"altune/go-api/internal/auth"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/config"
	"altune/go-api/internal/shared/events"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestShutdown_InFlightRequestContextSurvivesLifecycleCancel(t *testing.T) {
	lifecycle, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	release := make(chan struct{})
	result := make(chan error, 1)
	slow := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		result <- r.Context().Err()
	})
	a := &App{cfg: &config.Config{}}
	srv := httptest.NewUnstartedServer(slow)
	srv.Config.BaseContext = a.newServer(lifecycle, slow).BaseContext
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

func TestShutdown_SSEStreamEndsOnShutdownSignal(t *testing.T) {
	uid := shared.NewUserId(uuid.New())
	done := make(chan struct{})
	h := newSSEHandler(events.NewInProcessBus(), 0).withShutdown(done)
	h.heartbeat = time.Hour
	r := httptest.NewRequest(http.MethodGet, "/v1/events", nil).
		WithContext(auth.ContextWithUserID(context.Background(), uid))

	returned := make(chan struct{})
	go func() {
		h.ServeHTTP(httptest.NewRecorder(), r)
		close(returned)
	}()
	time.Sleep(20 * time.Millisecond)
	close(done)

	select {
	case <-returned:
	case <-time.After(2 * time.Second):
		t.Fatal("stream did not end on shutdown signal")
	}
}
