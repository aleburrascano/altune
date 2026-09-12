package app

import (
	"altune/go-api/internal/shared/config"
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
)

type countingTransport struct {
	calls atomic.Int32
}

func (c *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	c.calls.Add(1)
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader("{}")),
		Header:     make(http.Header),
	}, nil
}

func TestReRun_rejectsEmptyQueryWithoutFanningOut(t *testing.T) {
	ct := &countingTransport{}
	rr := &reRunner{
		cfg:              &config.Config{},
		behavioralScores: func() map[string]float64 { return nil },
		transport:        ct,
	}

	_, err := rr.ReRun(context.Background(), "", nil)
	if err == nil {
		t.Fatal("want validation error for empty query, got nil")
	}
	if n := ct.calls.Load(); n != 0 {
		t.Errorf("empty query must be rejected before fan-out, got %d provider calls", n)
	}
}
