package main

import (
	"bytes"
	"strings"
	"testing"
)

func statusFor(edit func(*statusRow)) statusRow {
	row := statusRow{
		TrackID: "t", Artist: "Drake", Title: "In My Feelings", AudioVersionAtApply: text("v1"), AudioVersion: text("v1"),
		OldFileSeconds: number(480), FileSeconds: number(218), CatalogSeconds: 218,
	}
	edit(&row)
	return row
}

func TestStatusRowOutcome(t *testing.T) {
	cases := []struct {
		name string
		edit func(*statusRow)
		want outcome
	}{
		{"job still in the queue", func(r *statusRow) { r.InFlight = true }, outcomeQueued},
		{"queued even when the audio was swapped by an earlier attempt", func(r *statusRow) { r.InFlight = true; r.AudioVersion = text("v2") }, outcomeQueued},
		{"audio never replaced", func(*statusRow) {}, outcomeNotReplaced},
		{"audio replaced and the length fits", func(r *statusRow) { r.AudioVersion = text("v2") }, outcomeFits},
		{"audio replaced and still off", func(r *statusRow) { r.AudioVersion = text("v2"); r.Mismatch = true }, outcomeStillOff},
		{"audio version was empty before the replace", func(r *statusRow) { r.AudioVersionAtApply = nil; r.AudioVersion = text("v2") }, outcomeFits},
		{"audio version still empty", func(r *statusRow) { r.AudioVersionAtApply = nil; r.AudioVersion = nil }, outcomeNotReplaced},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := statusFor(tc.edit).outcome(); got != tc.want {
				t.Fatalf("got %q want %q", got, tc.want)
			}
		})
	}
}

func TestWriteStatus_ListenListHoldsOnlyWhatStillMismatches(t *testing.T) {
	fits := statusFor(func(r *statusRow) { r.Title = "Fits"; r.AudioVersion = text("v2") })
	off := statusFor(func(r *statusRow) {
		r.Title = "Off"
		r.AudioVersion = text("v2")
		r.Mismatch = true
		r.FileSeconds = number(150)
	})
	kept := statusFor(func(r *statusRow) { r.Title = "Kept" })
	queued := statusFor(func(r *statusRow) { r.Title = "Queued"; r.InFlight = true })
	var out bytes.Buffer
	writeStatus(&out, []statusRow{fits, off, kept, queued})
	_, listen, found := strings.Cut(out.String(), "listen list")
	if !found || !strings.Contains(listen, "(2)") ||
		!strings.Contains(listen, "Drake - Off: 150s vs 218s (replaced, still off)") ||
		!strings.Contains(listen, "Drake - Kept: 480s vs 218s (not replaced)") ||
		strings.Contains(listen, "Fits") || strings.Contains(listen, "Queued") {
		t.Fatalf("got:\n%s", out.String())
	}
}
