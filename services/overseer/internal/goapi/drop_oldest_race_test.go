package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

const racedBurst = 20000

type pipeOnce struct {
	body   *io.PipeReader
	dialed atomic.Bool
}

func (p *pipeOnce) RoundTrip(req *http.Request) (*http.Response, error) {
	if p.dialed.Swap(true) {
		return nil, errors.New("dial tcp: connection refused")
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{"Content-Type": {"text/event-stream"}},
		Body:       p.body,
		Request:    req,
	}, nil
}

type racedFeed struct {
	t    *testing.T
	w    *io.PipeWriter
	sent int
}

func (f *racedFeed) send(n int) {
	f.t.Helper()
	var frames strings.Builder
	for range n {
		frames.WriteString(`data: {"type":"raced","subject":"` + strconv.Itoa(f.sent) + "\"}\n\n")
		f.sent++
	}
	if _, err := io.WriteString(f.w, frames.String()); err != nil {
		f.t.Fatalf("write frames: %v", err)
	}
}

func pipedConsumer(t *testing.T) (*racedFeed, *goapi.Consumer) {
	t.Helper()
	body, writer := io.Pipe()
	c, err := goapi.NewConsumer("https://api.altune.test", goapi.StaticTokenSource(testToken),
		goapi.WithConsumerHTTPClient(&http.Client{Transport: &pipeOnce{body: body}}),
		goapi.WithBackoff(&fastBackoff{wait: time.Hour}))
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := runConsumer(t, c, ctx)
	t.Cleanup(func() { cancel(); _ = writer.Close(); <-done })
	return &racedFeed{t: t, w: writer}, c
}

func drainFromBucket(c *goapi.Consumer) []goapi.Event { return goapi.DrainPending(c, c.Events()) }

func TestConcurrentDrainAgainstASaturatedBufferLosesOnlyWhatItDropped(t *testing.T) {
	feed, c := pipedConsumer(t)
	var received atomic.Int64
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Go(func() {
		for {
			select {
			case <-stop:
				return
			default:
				received.Add(int64(len(drainFromBucket(c))))
			}
		}
	})

	for feed.sent < racedBurst {
		feed.send(min(64, racedBurst-feed.sent))
	}
	eventuallyWithin(t, "every sent event either received or counted dropped", 10*time.Second, func() bool {
		return received.Load()+int64(c.Dropped()) >= racedBurst
	})
	close(stop)
	wg.Wait()
	total := received.Load() + int64(len(drainFromBucket(c)))

	if got := total + int64(c.Dropped()); got != racedBurst {
		t.Fatalf("received %d + dropped %d = %d, want every one of the %d sent", total, c.Dropped(), got, racedBurst)
	}
}
