package goapi

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type healthPublisher interface {
	statusSource
	markUp()
	markFailed(err error)
	markDropped(err error)
}

func healthPublishers(t *testing.T) map[string]healthPublisher {
	t.Helper()
	events, err := NewConsumer("https://api.altune.test", StaticTokenSource("op-token"))
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	logs, err := NewLogsConsumer("https://api.altune.test", StaticTokenSource("op-token"))
	if err != nil {
		t.Fatalf("NewLogsConsumer: %v", err)
	}
	return map[string]healthPublisher{"events": events, "logs": logs}
}

func TestStreamStatusNeverPairsAStatusWithTheWrongReason(t *testing.T) {
	for name, c := range healthPublishers(t) {
		t.Run(name, func(t *testing.T) {
			refused := &SourceDownError{Op: "GET /stream", Err: errors.New("connection refused")}
			stop := make(chan struct{})
			writerDone := make(chan struct{})
			go func() {
				defer close(writerDone)
				for {
					select {
					case <-stop:
						return
					default:
					}
					c.markFailed(refused)
					c.markUp()
					c.markDropped(refused)
					c.markUp()
				}
			}()
			var mismatches atomic.Int64
			var readers sync.WaitGroup
			for range 2 {
				readers.Go(func() {
					for range 100_000 {
						status, reason := StreamStatus(c)
						if (status == StatusUp) != (status.PanelReason(reason) == "") {
							mismatches.Add(1)
						}
					}
				})
			}
			readers.Wait()
			close(stop)
			<-writerDone
			if n := mismatches.Load(); n > 0 {
				t.Fatalf("readers saw %d status/reason pairs that never coexisted", n)
			}
		})
	}
}

type fixedBackoff time.Duration

func (b fixedBackoff) Backoff(int) time.Duration { return time.Duration(b) }

func TestOutageLongerThanGraceReportsDownBeforeTheNextRedial(t *testing.T) {
	builds := map[string]func(t *testing.T, transport *pipeTransport) streamConsumer{
		"events": func(t *testing.T, transport *pipeTransport) streamConsumer {
			t.Helper()
			c, err := NewConsumer("https://api.altune.test", StaticTokenSource("op-token"),
				WithConsumerHTTPClient(&http.Client{Transport: transport}),
				WithBackoff(fixedBackoff(defaultBackoffMax)))
			if err != nil {
				t.Fatalf("NewConsumer: %v", err)
			}
			return c
		},
		"logs": func(t *testing.T, transport *pipeTransport) streamConsumer {
			t.Helper()
			c, err := NewLogsConsumer("https://api.altune.test", StaticTokenSource("op-token"),
				WithLogsHTTPClient(&http.Client{Transport: transport}),
				WithLogsBackoff(fixedBackoff(defaultBackoffMax)))
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
				if got := c.Status(); got != StatusConnecting {
					t.Fatalf("inside the grace window: status %v, want connecting", got)
				}

				time.Sleep(2 * time.Second)
				synctest.Wait()
				if dials := transport.dialCount(); dials != 1 {
					t.Fatalf("dials = %d, want no redial yet under a %v backoff", dials, defaultBackoffMax)
				}
				if got, reason := StreamStatus(c); got != StatusDown || reason != ReasonDown {
					t.Fatalf("past the grace with no redial: status %v reason %q, want down/down", got, reason)
				}
			})
		})
	}
}
