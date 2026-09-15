package goapi

import (
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
