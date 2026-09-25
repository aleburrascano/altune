package goapi_test

import (
	"altune/overseer/internal/goapi"
	"context"
	"math"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"
)

const (
	undrainedBurst  = 1000
	streamBufferCap = 256
)

func TestConsumerKeepsNewestEventsWhenNobodyDrains(t *testing.T) {
	stub := &stubSSE{holdOpen: true}
	for i := range undrainedBurst {
		stub.events = append(stub.events, goapi.Event{Type: "burst", Timestamp: time.Now().UTC(), Subject: strconv.Itoa(i)})
	}
	srv := httptest.NewServer(stub)
	defer srv.Close()
	c, _ := newConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	eventually(t, "reader consumed the whole burst without a drainer", func() bool {
		return c.Dropped() == undrainedBurst-streamBufferCap
	})

	retained := drainBuffered(c.Events())
	if len(retained) != streamBufferCap {
		t.Fatalf("buffer held %d events, want %d", len(retained), streamBufferCap)
	}
	for i, ev := range retained {
		if want := strconv.Itoa(undrainedBurst - streamBufferCap + i); ev.Subject != want {
			t.Fatalf("retained[%d] = %q, want %q (the newest events, oldest first)", i, ev.Subject, want)
		}
	}
	if c.Status() != goapi.StatusUp {
		t.Fatalf("status = %v after a lossy burst, want up", c.Status())
	}
}

func TestLogsConsumerKeepsNewestRecordsWhenNobodyDrains(t *testing.T) {
	stub := &stubLogSSE{holdOpen: true}
	for i := range undrainedBurst {
		stub.records = append(stub.records, goapi.LogRecord{Level: "INFO", Message: strconv.Itoa(i)})
	}
	srv := httptest.NewServer(stub)
	defer srv.Close()
	c, _ := newLogsConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runLogsConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	eventually(t, "reader consumed the whole burst without a drainer", func() bool {
		return c.Dropped() == undrainedBurst-streamBufferCap
	})

	retained := drainBuffered(c.Records())
	if len(retained) != streamBufferCap {
		t.Fatalf("buffer held %d records, want %d", len(retained), streamBufferCap)
	}
	for i, rec := range retained {
		if want := strconv.Itoa(undrainedBurst - streamBufferCap + i); rec.Message != want {
			t.Fatalf("retained[%d] = %q, want %q (the newest records, oldest first)", i, rec.Message, want)
		}
	}
}

func TestConsumerDropsNothingWhileTheBufferHasRoom(t *testing.T) {
	stub := &stubSSE{holdOpen: true}
	for i := range streamBufferCap {
		stub.events = append(stub.events, goapi.Event{Type: "burst", Subject: strconv.Itoa(i)})
	}
	srv := httptest.NewServer(stub)
	defer srv.Close()
	c, _ := newConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runConsumer(t, c, ctx)
	defer func() { cancel(); <-done }()

	eventually(t, "buffer filled to capacity", func() bool { return len(c.Events()) == streamBufferCap })
	if got := c.Dropped(); got != 0 {
		t.Fatalf("Dropped() = %d with the buffer exactly full, want 0", got)
	}
}

func TestShutdownWithAFullUndrainedBufferReturnsPromptly(t *testing.T) {
	stub := &stubSSE{holdOpen: true}
	for i := range undrainedBurst {
		stub.events = append(stub.events, goapi.Event{Type: "burst", Subject: strconv.Itoa(i)})
	}
	srv := httptest.NewServer(stub)
	defer srv.Close()
	c, _ := newConsumer(t, srv.URL)
	ctx, cancel := context.WithCancel(context.Background())
	done := runConsumer(t, c, ctx)
	eventually(t, "buffer saturated", func() bool { return c.Dropped() > 0 })

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after cancel with a full, undrained buffer")
	}
}

type fixedDrops int

func (f fixedDrops) Dropped() int { return int(f) }

func TestTotalDroppedAddsStreamDropsToEvictions(t *testing.T) {
	cases := []struct {
		name    string
		src     any
		evicted int
		want    int
	}{
		{"source without a drop count", struct{}{}, 5, 5},
		{"nil source", nil, 3, 3},
		{"stream and ring drops both counted", fixedDrops(7), 5, 12},
		{"stream drops alone", fixedDrops(9), 0, 9},
		{"sum saturates instead of wrapping", fixedDrops(math.MaxInt), 1, math.MaxInt},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := goapi.TotalDropped(tc.src, tc.evicted); got != tc.want {
				t.Fatalf("TotalDropped(%v, %d) = %d, want %d", tc.src, tc.evicted, got, tc.want)
			}
		})
	}
}

func drainBuffered[T any](ch <-chan T) []T {
	var buffered []T
	for {
		select {
		case item, ok := <-ch:
			if !ok {
				return buffered
			}
			buffered = append(buffered, item)
		default:
			return buffered
		}
	}
}
