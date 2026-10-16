package upgrade

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/acquisition/service/grade"
	"errors"
	"testing"
)

func TestGate_AdmitFetched(t *testing.T) {
	const floor = 112
	muxed := ports.SourceFormat{ID: "18", Kbps: 96}
	audioOnly := ports.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    Gate
		grade   grade.Grade
		fetched ports.SourceFormat
		dryRun  bool
		wantErr error
	}{
		{"a muxed file at A takes a same-grade audio-only stream", Gate{Current: grade.A, Stored: muxed, FloorKbps: floor}, grade.A, audioOnly, false, nil},
		{"a muxed file at A refuses a lower-grade audio-only stream", Gate{Current: grade.A, Stored: muxed, FloorKbps: floor}, grade.B, audioOnly, false, ErrNotAnUpgrade},
		{"a muxed file at A refuses a same-grade muxed stream", Gate{Current: grade.A, Stored: muxed, FloorKbps: floor}, grade.A, muxed, false, ErrNotAnUpgrade},
		{"a muxed file at A refuses an audio-only stream under the floor", Gate{Current: grade.A, Stored: muxed, FloorKbps: floor}, grade.A, ports.SourceFormat{ID: "139", Kbps: 48}, false, ErrNotAnUpgrade},
		{"an audio-only file at A takes no same-grade stream", Gate{Current: grade.A}, grade.A, audioOnly, false, ErrNotAnUpgrade},
		{"a muxed file at B still takes a higher grade", Gate{Current: grade.B, Stored: muxed, FloorKbps: floor}, grade.A, muxed, false, nil},
		{"a dry run reports the swap without making it", Gate{Current: grade.A, Stored: muxed, FloorKbps: floor, DryRun: true}, grade.A, audioOnly, true, ErrWouldUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(tc.grade, tc.fetched)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("AdmitFetched = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGate_RefetchesCurrentSource(t *testing.T) {
	muxed := ports.SourceFormat{ID: "18", Kbps: 96}
	if !(Gate{Current: grade.A, Stored: muxed, FloorKbps: 112}).RefetchesCurrentSource() {
		t.Error("a muxed file must be refetched from its own source")
	}
	if (Gate{Current: grade.A, Stored: ports.SourceFormat{ID: "140", Kbps: 130}, FloorKbps: 112}).RefetchesCurrentSource() {
		t.Error("an audio-only file above the floor must not be refetched")
	}
	if (Gate{Current: grade.A, Stored: muxed}).RefetchesCurrentSource() {
		t.Error("a zero floor switches the rescue off")
	}
}
