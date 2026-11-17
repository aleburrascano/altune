package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

const (
	dialsPastGrace = 20
	flipsToWatch   = 20000
)

type switchBackoff struct {
	wait atomic.Int64
}

func (b *switchBackoff) Backoff(int) time.Duration { return time.Duration(b.wait.Load()) }

type flappingTransport struct {
	dials   atomic.Int64
	backoff *switchBackoff
	tight   chan struct{}
}

func newFlappingTransport() *flappingTransport {
	f := &flappingTransport{backoff: &switchBackoff{}, tight: make(chan struct{})}
	f.backoff.wait.Store(int64(time.Second))
	return f
}

func (f *flappingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	n := f.dials.Add(1)
	if n == dialsPastGrace {
		f.backoff.wait.Store(0)
		close(f.tight)
	}
	if n%2 == 0 {
		return nil, errors.New("dial tcp: connection refused")
	}
	return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
}

func TestStreamStatusNeverPairsAStatusWithTheWrongReason(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		transport := newFlappingTransport()
		c, err := goapi.NewConsumer("https://api.altune.test", goapi.StaticTokenSource(testToken),
			goapi.WithConsumerHTTPClient(&http.Client{Transport: transport}), goapi.WithBackoff(transport.backoff))
		if err != nil {
			t.Fatalf("NewConsumer: %v", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := runConsumer(t, c, ctx)
		defer func() { cancel(); <-done }()
		<-transport.tight

		var mismatches, downs atomic.Int64
		var readers sync.WaitGroup
		for range 2 {
			readers.Go(func() {
				for transport.dials.Load() < dialsPastGrace+flipsToWatch {
					status, reason := goapi.StreamStatus(c)
					if status == goapi.StatusDown {
						downs.Add(1)
					}
					if (status == goapi.StatusUp) != (status.PanelReason(reason) == "") {
						mismatches.Add(1)
					}
				}
			})
		}
		readers.Wait()
		if n := mismatches.Load(); n > 0 {
			t.Fatalf("readers saw %d status/reason pairs that never coexisted", n)
		}
		if downs.Load() == 0 {
			t.Fatal("readers never saw the stream down; the flapping never left the grace window")
		}
	})
}

func TestErrorMessagesNameTheCorrelationIDOnlyWhenThereIsOne(t *testing.T) {
	cause := errors.New("connection refused")
	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "source down with id",
			err:  &goapi.SourceDownError{Op: "GET /health", Err: cause, CorrID: "ab12"},
			want: "goapi: source down during GET /health: connection refused (corr_id=ab12)",
		},
		{
			name: "source down without id",
			err:  &goapi.SourceDownError{Op: "GET /health", Err: cause},
			want: "goapi: source down during GET /health: connection refused",
		},
		{
			name: "api error with id",
			err:  &goapi.APIError{Op: "GET /observe/health", StatusCode: http.StatusBadGateway, Body: "bad gateway", CorrID: "srvecho01"},
			want: "goapi: GET /observe/health: unexpected status 502: bad gateway (corr_id=srvecho01)",
		},
		{
			name: "api error without id",
			err:  &goapi.APIError{Op: "GET /observe/health", StatusCode: http.StatusBadGateway, Body: "bad gateway"},
			want: "goapi: GET /observe/health: unexpected status 502: bad gateway",
		},
		{
			name: "token error",
			err:  &goapi.TokenError{Op: "acquire read-only token", Err: cause},
			want: "goapi: acquire read-only token: connection refused",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Fatalf("Error() = %q, want %q", got, tc.want)
			}
		})
	}
}
