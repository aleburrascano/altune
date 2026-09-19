package goapi

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

// TestSSEDecoderParsesWireForm drives the unexported decoder directly: it must
// pull the data frames go-api emits, tolerate comments/keep-alives and
// multi-line data, skip a malformed frame without tearing down, and end on EOF.
func TestSSEDecoderParsesWireForm(t *testing.T) {
	stream := strings.Join([]string{
		": keep-alive comment",        // comment line, ignored
		"data: {\"type\":\"a\"}",      // one clean frame
		"",                            //
		"event: ignored",              // non-data field, ignored
		"data: {\"type\":\"b\",",      // multi-line data...
		"data:  \"subject\":\"two\"}", // ...folded together (note the extra space is stripped once)
		"",                            //
		"data: not json",              // malformed → skipped, not fatal
		"",                            //
		"data: {\"type\":\"c\"}",      // survives after the bad frame
		"",                            //
	}, "\n") + "\n"

	dec := newSSEDecoder(strings.NewReader(stream))

	first, err := dec.next()
	if err != nil || first.Type != "a" {
		t.Fatalf("frame 1 = %+v, err=%v; want type a", first, err)
	}
	second, err := dec.next()
	if err != nil || second.Type != "b" || second.Subject != "two" {
		t.Fatalf("frame 2 = %+v, err=%v; want type b / subject two", second, err)
	}
	third, err := dec.next()
	if err != nil || third.Type != "c" {
		t.Fatalf("frame 3 = %+v, err=%v; want type c (malformed frame must be skipped)", third, err)
	}
	if _, err := dec.next(); !errors.Is(err, io.EOF) {
		t.Fatalf("end of stream err = %v, want io.EOF", err)
	}
}

// TestSSEDecoderCarriesCorrelationID proves the wire corr_id go-api stamps on an
// event (enabler #1946) is decoded onto Event.CorrID, so a live event ties back to
// the go-api request that produced it.
func TestSSEDecoderCarriesCorrelationID(t *testing.T) {
	stream := "data: {\"type\":\"track.played\",\"corr_id\":\"a1b2c3d4\"}\n\n"
	dec := newSSEDecoder(strings.NewReader(stream))

	ev, err := dec.next()
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if ev.CorrID != "a1b2c3d4" {
		t.Fatalf("CorrID = %q, want a1b2c3d4", ev.CorrID)
	}
}

// TestSSEDecoderDropsSpoofedCorrelationID is the wire-injection guard: a corr_id
// carrying a newline (log forgery) or exceeding the length cap — the shape a
// compromised or buggy upstream could inject — is dropped to empty rather than
// carried into a log line or panel.
func TestSSEDecoderDropsSpoofedCorrelationID(t *testing.T) {
	cases := map[string]string{
		"newline injection": "evil\ninjected line",
		"angle brackets":    "<script>",
		"over length cap":   strings.Repeat("a", maxCorrelationIDLen+1),
	}
	for name, corr := range cases {
		t.Run(name, func(t *testing.T) {
			raw, err := json.Marshal(map[string]string{"type": "x", "corr_id": corr})
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			dec := newSSEDecoder(strings.NewReader("data: " + string(raw) + "\n\n"))
			ev, err := dec.next()
			if err != nil {
				t.Fatalf("next: %v", err)
			}
			if ev.CorrID != "" {
				t.Fatalf("CorrID = %q, want empty (spoofed value must be dropped)", ev.CorrID)
			}
		})
	}
}

// TestSSEDecoderRejectsOverlongFrame proves a single runaway frame cannot exhaust
// memory: it surfaces as an error (which the consumer treats as a dropped stream)
// rather than being buffered without bound.
func TestSSEDecoderRejectsOverlongFrame(t *testing.T) {
	huge := "data: " + strings.Repeat("x", maxEventBytes+1)
	dec := newSSEDecoder(strings.NewReader(huge))
	if _, err := dec.next(); err == nil {
		t.Fatal("overlong frame returned nil error; the frame size was not bounded")
	}
}

// TestSSEDecoderRejectsUnterminatedMultilineFrame proves the frame accumulator is
// bounded across lines, not only per line: an endless run of short data: lines
// with no blank separator (a runaway upstream, or a proxy that strips separators)
// must surface errFrameTooLarge and let the consumer reconnect, rather than
// growing the frame buffer until Overseer is OOM-killed. Each line here is far
// under the per-line scanner cap, so only a cross-line bound can stop it.
func TestSSEDecoderRejectsUnterminatedMultilineFrame(t *testing.T) {
	line := "data: " + strings.Repeat("x", 1024) + "\n"
	repeats := (maxEventBytes / 1024) + 8 // enough folded lines to exceed the cap
	dec := newSSEDecoder(strings.NewReader(strings.Repeat(line, repeats)))
	if _, err := dec.next(); !errors.Is(err, errFrameTooLarge) {
		t.Fatalf("unterminated multi-line frame err = %v, want errFrameTooLarge (accumulator not bounded across lines)", err)
	}
}
