package handler

import (
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamSSEEndsAtMaxLifetime(t *testing.T) {
	prev := streamMaxLifetime
	streamMaxLifetime = 50 * time.Millisecond
	t.Cleanup(func() { streamMaxLifetime = prev })

	ch := make(chan string)
	done := make(chan struct{})
	go func() {
		defer close(done)
		streamSSE(httptest.NewRecorder(), httptest.NewRequest("GET", "/logs/stream", nil), ch)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stream outlived its max lifetime")
	}
}
