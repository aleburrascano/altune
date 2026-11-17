package goapihttp_test

import (
	"altune/overseer/internal/goapi"
	goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"
	"context"
	"errors"
	"io"
	"net/http"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

const (
	reconnectGrace    = 10 * time.Second
	streamIdleTimeout = 60 * time.Second
	reasonConnecting  = "connecting"
)

type fixedBackoff time.Duration

func (b fixedBackoff) Backoff(int) time.Duration { return time.Duration(b) }

func startConsumer(c streamConsumer) func() {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Run(ctx)
	}()
	return func() {
		cancel()
		<-done
	}
}

func waitUntil(t *testing.T, cond func() bool, what string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

type pipeTransport struct {
	mu       sync.Mutex
	dials    int
	refusing bool
	hangUp   bool
	writers  []*io.PipeWriter
}

func (p *pipeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.dials++
	if p.refusing {
		return nil, errors.New("dial tcp: connection refused")
	}
	if p.hangUp {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Request: req}, nil
	}
	body, writer := io.Pipe()
	p.writers = append(p.writers, writer)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       body,
		Request:    req,
	}, nil
}

func (p *pipeTransport) refuse(refusing bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refusing = refusing
}

func (p *pipeTransport) acceptThenHangUp() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hangUp = true
}

func (p *pipeTransport) dialCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.dials
}

func (p *pipeTransport) latestStream() *io.PipeWriter {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.writers[len(p.writers)-1]
}

type streamConsumer interface {
	Run(ctx context.Context) error
	Status() goapi.Status
	LastError() error
}

type consumerCase struct {
	name  string
	build func(t *testing.T, transport *pipeTransport) streamConsumer
}

func streamConsumerCases() []consumerCase {
	return []consumerCase{
		{"events", func(t *testing.T, transport *pipeTransport) streamConsumer {
			t.Helper()
			c, err := goapihttp.NewConsumer("https://api.altune.test", goapi.StaticTokenSource("op-token"),
				goapihttp.WithConsumerHTTPClient(&http.Client{Transport: transport}))
			if err != nil {
				t.Fatalf("NewConsumer: %v", err)
			}
			return c
		}},
		{"logs", func(t *testing.T, transport *pipeTransport) streamConsumer {
			t.Helper()
			c, err := goapihttp.NewLogsConsumer("https://api.altune.test", goapi.StaticTokenSource("op-token"),
				goapihttp.WithLogsHTTPClient(&http.Client{Transport: transport}))
			if err != nil {
				t.Fatalf("NewLogsConsumer: %v", err)
			}
			return c
		}},
	}
}

func runStreamConsumer(t *testing.T, c streamConsumer) func() {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = c.Run(ctx)
	}()
	synctest.Wait()
	if got := c.Status(); got != goapi.StatusUp {
		t.Fatalf("status after first connect = %v, want up", got)
	}
	return func() {
		cancel()
		<-done
	}
}

func streamPanelReason(c streamConsumer) string {
	return c.Status().PanelReason(goapi.Classify(c.LastError()))
}

func TestStreamDropReportsConnectingUntilReconnectedNeverSourceDown(t *testing.T) {
	for _, tc := range streamConsumerCases() {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transport := &pipeTransport{}
				c := tc.build(t, transport)
				stop := runStreamConsumer(t, c)
				defer stop()

				transport.refuse(true)
				_ = transport.latestStream().Close()
				synctest.Wait()
				if got, reason := c.Status(), streamPanelReason(c); got != goapi.StatusConnecting || reason != reasonConnecting {
					t.Fatalf("right after the drop: status %v reason %q, want connecting/connecting", got, reason)
				}

				for elapsed := time.Duration(0); elapsed < 8*time.Second; elapsed += 100 * time.Millisecond {
					time.Sleep(100 * time.Millisecond)
					synctest.Wait()
					if got := c.Status(); got != goapi.StatusConnecting {
						t.Fatalf("%v into refused reconnects: status %v, want connecting", elapsed, got)
					}
				}
				if transport.dialCount() < 3 {
					t.Fatalf("dials = %d, want reconnect attempts during the outage", transport.dialCount())
				}

				transport.refuse(false)
				time.Sleep(10 * time.Second)
				synctest.Wait()
				if got, reason := c.Status(), streamPanelReason(c); got != goapi.StatusUp || reason != "" {
					t.Fatalf("after go-api returns: status %v reason %q, want up with no reason", got, reason)
				}
				if c.Status().PanelState() != "live" {
					t.Fatalf("panel state after reconnect = %q, want live", c.Status().PanelState())
				}
			})
		})
	}
}

func TestStreamReconnectsFailingPastGraceReportDown(t *testing.T) {
	for _, tc := range streamConsumerCases() {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transport := &pipeTransport{}
				c := tc.build(t, transport)
				stop := runStreamConsumer(t, c)
				defer stop()

				transport.refuse(true)
				_ = transport.latestStream().Close()
				time.Sleep(reconnectGrace - time.Second)
				synctest.Wait()
				if got := c.Status(); got != goapi.StatusConnecting {
					t.Fatalf("inside the grace window: status %v, want connecting", got)
				}

				time.Sleep(20 * time.Second)
				synctest.Wait()
				if got, reason := c.Status(), streamPanelReason(c); got != goapi.StatusDown || reason != goapi.ReasonDown {
					t.Fatalf("after reconnects failed past the grace: status %v reason %q, want down/down", got, reason)
				}
				if c.Status().PanelState() != "source_down" {
					t.Fatalf("panel state = %q, want source_down", c.Status().PanelState())
				}
			})
		})
	}
}

func TestSilentStreamIsClosedAndRedialledAfterIdleTimeout(t *testing.T) {
	for _, tc := range streamConsumerCases() {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transport := &pipeTransport{}
				c := tc.build(t, transport)
				stop := runStreamConsumer(t, c)
				defer stop()
				silent := transport.latestStream()

				time.Sleep(streamIdleTimeout - time.Second)
				synctest.Wait()
				if dials := transport.dialCount(); dials != 1 || c.Status() != goapi.StatusUp {
					t.Fatalf("before the idle timeout: dials %d status %v, want 1/up", dials, c.Status())
				}

				time.Sleep(time.Second + 100*time.Millisecond)
				synctest.Wait()
				if _, err := silent.Write([]byte(": late\n")); !errors.Is(err, io.ErrClosedPipe) {
					t.Fatalf("write to the silent stream = %v, want it closed by the watchdog", err)
				}
				if got, err := c.Status(), c.LastError(); got != goapi.StatusConnecting || !errors.Is(err, goapihttp.ErrStreamIdle) {
					t.Fatalf("after the idle timeout: status %v lastErr %v, want connecting/%v", got, err, goapihttp.ErrStreamIdle)
				}

				time.Sleep(time.Second)
				synctest.Wait()
				if dials := transport.dialCount(); dials != 2 || c.Status() != goapi.StatusUp {
					t.Fatalf("after redial: dials %d status %v, want 2/up", dials, c.Status())
				}
			})
		})
	}
}

func TestKeepaliveCommentsKeepAStreamAlivePastIdleTimeout(t *testing.T) {
	for _, tc := range streamConsumerCases() {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transport := &pipeTransport{}
				c := tc.build(t, transport)
				stop := runStreamConsumer(t, c)
				defer stop()
				stream := transport.latestStream()

				for range 6 {
					time.Sleep(25 * time.Second)
					if _, err := stream.Write([]byte(": keepalive\n\n")); err != nil {
						t.Fatalf("keepalive write: %v", err)
					}
				}
				synctest.Wait()

				if dials := transport.dialCount(); dials != 1 || c.Status() != goapi.StatusUp {
					t.Fatalf("after 150s of keepalives only: dials %d status %v, want 1/up", dials, c.Status())
				}
			})
		})
	}
}

func TestStreamThatDiesOnEveryReconnectReportsDownPastGrace(t *testing.T) {
	for _, tc := range streamConsumerCases() {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transport := &pipeTransport{}
				c := tc.build(t, transport)
				stop := runStreamConsumer(t, c)
				defer stop()

				transport.acceptThenHangUp()
				_ = transport.latestStream().Close()
				time.Sleep(reconnectGrace - time.Second)
				synctest.Wait()
				if got := c.Status(); got != goapi.StatusConnecting {
					t.Fatalf("inside the grace window: status %v, want connecting", got)
				}

				time.Sleep(20 * time.Second)
				synctest.Wait()
				if got, reason := c.Status(), goapi.Classify(c.LastError()); got != goapi.StatusDown || reason != goapi.ReasonDown {
					t.Fatalf("after every reconnect died for 20s: status %v reason %q, want down/down", got, reason)
				}
			})
		})
	}
}

func TestOutageLongerThanGraceReportsDownBeforeTheNextRedial(t *testing.T) {
	const redialBackoff = 30 * time.Second
	builds := map[string]func(t *testing.T, transport *pipeTransport) streamConsumer{
		"events": func(t *testing.T, transport *pipeTransport) streamConsumer {
			t.Helper()
			c, err := goapihttp.NewConsumer("https://api.altune.test", goapi.StaticTokenSource("op-token"),
				goapihttp.WithConsumerHTTPClient(&http.Client{Transport: transport}),
				goapihttp.WithBackoff(fixedBackoff(redialBackoff)))
			if err != nil {
				t.Fatalf("NewConsumer: %v", err)
			}
			return c
		},
		"logs": func(t *testing.T, transport *pipeTransport) streamConsumer {
			t.Helper()
			c, err := goapihttp.NewLogsConsumer("https://api.altune.test", goapi.StaticTokenSource("op-token"),
				goapihttp.WithLogsHTTPClient(&http.Client{Transport: transport}),
				goapihttp.WithLogsBackoff(fixedBackoff(redialBackoff)))
			if err != nil {
				t.Fatalf("NewLogsConsumer: %v", err)
			}
			return c
		},
	}
	for name, build := range builds {
		t.Run(name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				transport := &pipeTransport{}
				c := build(t, transport)
				stop := runStreamConsumer(t, c)
				defer stop()

				transport.refuse(true)
				_ = transport.latestStream().Close()
				time.Sleep(reconnectGrace - time.Second)
				synctest.Wait()
				if got := c.Status(); got != goapi.StatusConnecting {
					t.Fatalf("inside the grace window: status %v, want connecting", got)
				}

				time.Sleep(2 * time.Second)
				synctest.Wait()
				if dials := transport.dialCount(); dials != 1 {
					t.Fatalf("dials = %d, want no redial yet under a %v backoff", dials, redialBackoff)
				}
				if got, reason := goapi.StreamStatus(c); got != goapi.StatusDown || reason != goapi.ReasonDown {
					t.Fatalf("past the grace with no redial: status %v reason %q, want down/down", got, reason)
				}
			})
		})
	}
}
