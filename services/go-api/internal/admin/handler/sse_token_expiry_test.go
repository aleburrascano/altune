package handler

import (
	"altune/go-api/internal/auth"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStreamSSEEndsAtTokenExpiry(t *testing.T) {
	req := httptest.NewRequest("GET", "/logs/stream", nil)
	req = req.WithContext(auth.ContextWithTokenExpiry(req.Context(), time.Now().Add(50*time.Millisecond)))

	ch := make(chan string)
	done := make(chan struct{})
	go func() {
		defer close(done)
		streamSSE(httptest.NewRecorder(), req, ch)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("admin stream outlived the token that authenticated it")
	}
}
