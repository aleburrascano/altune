package ports_test

import (
	"altune/go-api/internal/download"
	"testing"
)

func TestSourceFormat_IsLowBitrate(t *testing.T) {
	cases := []struct {
		name   string
		format download.SourceFormat
		floor  int
		want   bool
	}{
		{"the muxed fallback is low", download.SourceFormat{ID: "18", Kbps: 96}, 112, true},
		{"the muxed fallback is low whatever its reported bitrate", download.SourceFormat{ID: "18"}, 112, true},
		{"a bitrate under the floor is low", download.SourceFormat{ID: "139", Kbps: 96}, 112, true},
		{"a bitrate above the floor is not low", download.SourceFormat{ID: "140", Kbps: 130}, 112, false},
		{"a bitrate at the floor is not low", download.SourceFormat{ID: "140", Kbps: 112}, 112, false},
		{"an unknown bitrate on another format is not low", download.SourceFormat{ID: "140"}, 112, false},
		{"a zero floor never calls the muxed fallback low", download.SourceFormat{ID: "18", Kbps: 96}, 0, false},
		{"a zero floor never calls a low bitrate low", download.SourceFormat{ID: "139", Kbps: 48}, 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.format.IsLowBitrate(tc.floor); got != tc.want {
				t.Errorf("%+v.IsLowBitrate(%d) = %v, want %v", tc.format, tc.floor, got, tc.want)
			}
		})
	}
}
