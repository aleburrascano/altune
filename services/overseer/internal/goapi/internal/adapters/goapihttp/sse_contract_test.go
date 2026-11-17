package goapihttp_test

import (
	"altune/overseer/internal/goapi"
	goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func consumeStream(t *testing.T, stream string) (*goapihttp.Consumer, func()) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, stream)
	}))
	t.Cleanup(srv.Close)
	c, err := goapihttp.NewConsumer(srv.URL, goapi.StaticTokenSource("op-token"), goapihttp.WithBackoff(fixedBackoff(time.Hour)))
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}
	return c, startConsumer(c)
}

func nextEvent(t *testing.T, c *goapihttp.Consumer) goapi.Event {
	t.Helper()
	select {
	case ev := <-c.Events():
		return ev
	case <-time.After(5 * time.Second):
		t.Fatal("no event decoded from the stream")
		return goapi.Event{}
	}
}

func streamEndedWith(t *testing.T, c *goapihttp.Consumer) error {
	t.Helper()
	waitUntil(t, func() bool { return c.LastError() != nil }, "the stream to end")
	return c.LastError()
}

func TestSSEDecoderParsesWireForm(t *testing.T) {
	stream := strings.Join([]string{
		": keep-alive comment",
		"data: {\"type\":\"a\"}",
		"",
		"event: ignored",
		"data: {\"type\":\"b\",",
		"data:  \"subject\":\"two\"}",
		"",
		"data: not json",
		"",
		"data: {\"type\":\"c\"}",
		"",
	}, "\n") + "\n"
	c, stop := consumeStream(t, stream)
	defer stop()

	if first := nextEvent(t, c); first.Type != "a" {
		t.Fatalf("frame 1 = %+v; want type a", first)
	}
	if second := nextEvent(t, c); second.Type != "b" || second.Subject != "two" {
		t.Fatalf("frame 2 = %+v; want type b / subject two", second)
	}
	if third := nextEvent(t, c); third.Type != "c" {
		t.Fatalf("frame 3 = %+v; want type c (malformed frame must be skipped)", third)
	}
	if err := streamEndedWith(t, c); !errors.Is(err, io.EOF) {
		t.Fatalf("end of stream err = %v, want io.EOF", err)
	}
	if extra := len(c.Events()); extra != 0 {
		t.Fatalf("%d events past the last frame, want none", extra)
	}
}

func TestSSEDecoderCarriesCorrelationID(t *testing.T) {
	c, stop := consumeStream(t, "data: {\"type\":\"track.played\",\"corr_id\":\"a1b2c3d4\"}\n\n")
	defer stop()

	if ev := nextEvent(t, c); ev.CorrID != "a1b2c3d4" {
		t.Fatalf("CorrID = %q, want a1b2c3d4", ev.CorrID)
	}
}

func TestSSEDecoderDropsSpoofedCorrelationID(t *testing.T) {
	cases := map[string]string{
		"newline injection": "evil\ninjected line",
		"angle brackets":    "<script>",
		"over length cap":   strings.Repeat("a", 65),
	}
	for name, corr := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]string{"type": "x", "corr_id": corr})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			c, stop := consumeStream(t, "data: "+string(raw)+"\n\n")
			defer stop()

			if ev := nextEvent(t, c); ev.CorrID != "" {
				t.Fatalf("CorrID = %q, want empty (spoofed value must be dropped)", ev.CorrID)
			}
		})
	}
}

func TestSSEDecoderRejectsOverlongFrame(t *testing.T) {
	c, stop := consumeStream(t, "data: "+strings.Repeat("x", 1<<20+1))
	defer stop()

	if err := streamEndedWith(t, c); !errors.Is(err, bufio.ErrTooLong) {
		t.Fatalf("overlong frame err = %v, want bufio.ErrTooLong; the frame size was not bounded", err)
	}
	if n := len(c.Events()); n != 0 {
		t.Fatalf("%d events decoded from an overlong frame, want none", n)
	}
}

func TestSSEDecoderRejectsUnterminatedMultilineFrame(t *testing.T) {
	line := "data: " + strings.Repeat("x", 1024) + "\n"
	repeats := (1 << 20 / 1024) + 8
	c, stop := consumeStream(t, strings.Repeat(line, repeats))
	defer stop()

	if err := streamEndedWith(t, c); !errors.Is(err, goapihttp.ErrFrameTooLarge) {
		t.Fatalf("unterminated multi-line frame err = %v, want ErrFrameTooLarge (accumulator not bounded across lines)", err)
	}
}
