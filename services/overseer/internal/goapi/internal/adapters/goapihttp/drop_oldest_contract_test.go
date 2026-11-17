package goapihttp_test

import (
	"altune/overseer/internal/goapi"
	goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func serveFrames(t *testing.T, frames string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, frames)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestDrainPendingTakesAConsumersOwnDrainNotTheChannelItIsHanded(t *testing.T) {
	url := serveFrames(t, "data: {\"type\":\"streamed\",\"subject\":\"1\"}\n\ndata: {\"type\":\"streamed\",\"subject\":\"2\"}\n\n")
	c, err := goapihttp.NewConsumer(url, goapi.StaticTokenSource("op-token"), goapihttp.WithBackoff(fixedBackoff(time.Hour)))
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	stop := startConsumer(c)
	defer stop()
	waitUntil(t, func() bool { return len(c.Events()) == 2 }, "both frames to be buffered")

	decoy := make(chan goapi.Event, 1)
	decoy <- goapi.Event{Type: "decoy"}
	got := goapihttp.DrainPending(c, decoy)

	if len(got) != 2 || got[0].Subject != "1" || got[1].Subject != "2" {
		t.Fatalf("DrainPending(consumer, decoy) = %+v, want the consumer's two buffered events", got)
	}
	if len(decoy) != 1 {
		t.Fatal("DrainPending read the channel it was handed instead of the consumer's buffer")
	}
	if left := len(c.Events()); left != 0 {
		t.Fatalf("%d events left in the consumer after DrainPending, want none", left)
	}
}

func TestDrainPendingTakesALogsConsumersOwnDrainNotTheChannelItIsHanded(t *testing.T) {
	url := serveFrames(t, "data: {\"level\":\"INFO\",\"msg\":\"one\"}\n\ndata: {\"level\":\"WARN\",\"msg\":\"two\"}\n\n")
	c, err := goapihttp.NewLogsConsumer(url, goapi.StaticTokenSource("op-token"), goapihttp.WithLogsBackoff(fixedBackoff(time.Hour)))
	if err != nil {
		t.Fatalf("NewLogsConsumer: %v", err)
	}
	stop := startConsumer(c)
	defer stop()
	waitUntil(t, func() bool { return len(c.Records()) == 2 }, "both records to be buffered")

	decoy := make(chan goapi.LogRecord, 1)
	decoy <- goapi.LogRecord{Message: "decoy"}
	got := goapihttp.DrainPending(c, decoy)

	if len(got) != 2 || got[0].Message != "one" || got[1].Message != "two" {
		t.Fatalf("DrainPending(logs consumer, decoy) = %+v, want the consumer's two buffered records", got)
	}
	if len(decoy) != 1 {
		t.Fatal("DrainPending read the channel it was handed instead of the logs consumer's buffer")
	}
	if left := len(c.Records()); left != 0 {
		t.Fatalf("%d records left in the logs consumer after DrainPending, want none", left)
	}
}

func TestDrainPendingReadsTheChannelOfASourceWithNoDrainOfItsOwn(t *testing.T) {
	pending := make(chan goapi.Event, 2)
	pending <- goapi.Event{Type: "a"}
	pending <- goapi.Event{Type: "b"}

	got := goapihttp.DrainPending(struct{}{}, pending)

	if len(got) != 2 || got[0].Type != "a" || got[1].Type != "b" {
		t.Fatalf("DrainPending(plain source) = %+v, want both channel events in order", got)
	}
}
