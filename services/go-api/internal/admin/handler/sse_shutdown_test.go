package handler

import (
	"altune/go-api/internal/shared/logging"
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
)

var adminStreamPaths = []string{"/logs/stream", "/events/stream"}

func TestAdminStream_EndsWhenShutdownBegins(t *testing.T) {
	for _, path := range adminStreamPaths {
		t.Run(path, func(t *testing.T) {
			shutdown := make(chan struct{})
			returned, _ := serveStream(t, shutdown, path)

			select {
			case <-returned:
				t.Fatal("stream ended before shutdown began")
			case <-time.After(100 * time.Millisecond):
			}

			close(shutdown)
			select {
			case <-returned:
			case <-time.After(2 * time.Second):
				t.Fatal("stream held its handler open after shutdown began")
			}
		})
	}
}

func TestAdminStream_OpenedAfterShutdownEndsPromptly(t *testing.T) {
	for _, path := range adminStreamPaths {
		t.Run(path, func(t *testing.T) {
			shutdown := make(chan struct{})
			close(shutdown)

			returned, _ := serveStream(t, shutdown, path)
			select {
			case <-returned:
			case <-time.After(2 * time.Second):
				t.Fatal("stream opened during shutdown stayed open")
			}
		})
	}
}

func TestAdminStream_WithoutShutdownRunsUntilTheRequestEnds(t *testing.T) {
	for _, path := range adminStreamPaths {
		t.Run(path, func(t *testing.T) {
			returned, endRequest := serveStream(t, nil, path)

			select {
			case <-returned:
				t.Fatal("stream with no shutdown channel ended on its own")
			case <-time.After(100 * time.Millisecond):
			}

			endRequest()
			select {
			case <-returned:
			case <-time.After(2 * time.Second):
				t.Fatal("stream outlived its request")
			}
		})
	}
}

func serveStream(t *testing.T, shutdown <-chan struct{}, path string) (<-chan struct{}, context.CancelFunc) {
	t.Helper()
	_, feed := startEventFeed(t)
	r := chi.NewRouter()
	New(nil, logging.NewRingBuffer(8)).WithEventFeed(feed).WithShutdown(shutdown).RegisterData(r)
	reqCtx, endRequest := context.WithCancel(context.Background())
	req := httptest.NewRequestWithContext(reqCtx, http.MethodGet, path, nil)

	returned := make(chan struct{})
	go func() {
		defer close(returned)
		r.ServeHTTP(httptest.NewRecorder(), req)
	}()
	t.Cleanup(func() {
		endRequest()
		<-returned
	})
	return returned, endRequest
}
