package goapihttp_test

import (
	"altune/overseer/internal/goapi"
	goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"
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

func TestLogsStreamStatusNeverPairsAStatusWithTheWrongReason(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		transport := newFlappingTransport()
		c, err := goapihttp.NewLogsConsumer("https://api.altune.test", goapi.StaticTokenSource("op-token"),
			goapihttp.WithLogsHTTPClient(&http.Client{Transport: transport}), goapihttp.WithLogsBackoff(transport.backoff))
		if err != nil {
			t.Fatalf("NewLogsConsumer: %v", err)
		}
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			defer close(done)
			_ = c.Run(ctx)
		}()
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
