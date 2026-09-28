package goapi

import (
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
)

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

func TestSSEDecoderRejectsOverlongFrame(t *testing.T) {
	huge := "data: " + strings.Repeat("x", maxEventBytes+1)
	dec := newSSEDecoder(strings.NewReader(huge))
	if _, err := dec.next(); err == nil {
		t.Fatal("overlong frame returned nil error; the frame size was not bounded")
	}
}

func TestSSEDecoderRejectsUnterminatedMultilineFrame(t *testing.T) {
	line := "data: " + strings.Repeat("x", 1024) + "\n"
	repeats := (maxEventBytes / 1024) + 8
	dec := newSSEDecoder(strings.NewReader(strings.Repeat(line, repeats)))
	if _, err := dec.next(); !errors.Is(err, errFrameTooLarge) {
		t.Fatalf("unterminated multi-line frame err = %v, want errFrameTooLarge (accumulator not bounded across lines)", err)
	}
}
