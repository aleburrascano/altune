package replacement_test

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/download/internal/core/ports"
	"altune/go-api/internal/download/service/replacement"
	"errors"
	"testing"
)

func TestGate_AdmitFetched(t *testing.T) {
	const floor = 112
	muxed := ports.SourceFormat{ID: "18", Kbps: 96}
	audioOnly := ports.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    replacement.ReplacementGate
		grade   audioquality.Grade
		fetched ports.SourceFormat
		wantErr error
	}{
		{"a muxed file at A takes a same-grade audio-only stream", replacement.ReplacementGate{Current: audioquality.A, Stored: muxed, FloorKbps: floor}, audioquality.A, audioOnly, nil},
		{"a muxed file at A refuses a lower-grade audio-only stream", replacement.ReplacementGate{Current: audioquality.A, Stored: muxed, FloorKbps: floor}, audioquality.B, audioOnly, replacement.ErrNotAnUpgrade},
		{"a muxed file at A refuses a same-grade muxed stream", replacement.ReplacementGate{Current: audioquality.A, Stored: muxed, FloorKbps: floor}, audioquality.A, muxed, replacement.ErrNotAnUpgrade},
		{"a muxed file at A refuses an audio-only stream under the floor", replacement.ReplacementGate{Current: audioquality.A, Stored: muxed, FloorKbps: floor}, audioquality.A, ports.SourceFormat{ID: "139", Kbps: 48}, replacement.ErrNotAnUpgrade},
		{"an audio-only file at A takes no same-grade stream", replacement.ReplacementGate{Current: audioquality.A}, audioquality.A, audioOnly, replacement.ErrNotAnUpgrade},
		{"a muxed file at B still takes a higher grade", replacement.ReplacementGate{Current: audioquality.B, Stored: muxed, FloorKbps: floor}, audioquality.A, muxed, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(tc.grade, tc.fetched, 0)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("AdmitFetched = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGate_AdmitFetchedByCatalogLength(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    replacement.ReplacementGate
		grade   audioquality.Grade
		seconds float64
		wantErr error
	}{
		{"a file fitting the catalog replaces a missing stored edit", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: 231}, audioquality.B, 285, nil},
		{"a fitting stored file keeps the tie, fetched fitting", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: 284}, audioquality.B, 285, replacement.ErrNotAnUpgrade},
		{"a fitting stored file keeps the tie, fetched missing", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: 284}, audioquality.B, 231, replacement.ErrNotAnUpgrade},
		{"a fetched file that also misses is refused", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: 231}, audioquality.B, 231, replacement.ErrNotAnUpgrade},
		{"a lower grade is refused", replacement.ReplacementGate{Current: audioquality.A, CatalogSeconds: 285, StoredSeconds: 231}, audioquality.B, 285, replacement.ErrNotAnUpgrade},
		{"an unknown catalog length is refused", replacement.ReplacementGate{Current: audioquality.B, StoredSeconds: 231}, audioquality.B, 285, replacement.ErrNotAnUpgrade},
		{"an unknown stored length is refused", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285}, audioquality.B, 285, replacement.ErrNotAnUpgrade},
		{"an unknown fetched length is refused", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: 231}, audioquality.B, 0, replacement.ErrNotAnUpgrade},
		{"a zero catalog length is refused even beside a tiny fetched length", replacement.ReplacementGate{Current: audioquality.B, StoredSeconds: 231}, audioquality.B, 3, replacement.ErrNotAnUpgrade},
		{"a zero fetched length is refused even beside a tiny catalog length", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 4, StoredSeconds: 231}, audioquality.B, 0, replacement.ErrNotAnUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(tc.grade, fmt140, tc.seconds)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("AdmitFetched = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGate_RefetchesCurrentSource(t *testing.T) {
	muxed := ports.SourceFormat{ID: "18", Kbps: 96}
	if !(replacement.ReplacementGate{Current: audioquality.A, Stored: muxed, FloorKbps: 112}).RefetchesCurrentSource() {
		t.Error("a muxed file must be refetched from its own source")
	}
	if (replacement.ReplacementGate{Current: audioquality.A, Stored: ports.SourceFormat{ID: "140", Kbps: 130}, FloorKbps: 112}).RefetchesCurrentSource() {
		t.Error("an audio-only file above the floor must not be refetched")
	}
	if (replacement.ReplacementGate{Current: audioquality.A, Stored: muxed}).RefetchesCurrentSource() {
		t.Error("a zero floor switches the rescue off")
	}
}

func TestGate_AdmitFetchedByCatalogLengthAtTheToleranceEdges(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    replacement.ReplacementGate
		seconds float64
		wantErr error
	}{
		{"a fetched file exactly 12 s long fits and is admitted", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 400, StoredSeconds: 300}, 412, nil},
		{"a fetched file exactly 12 s short fits and is admitted", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 400, StoredSeconds: 300}, 388, nil},
		{"a fetched file just over 12 s long misses and is refused", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 400, StoredSeconds: 300}, 412.1, replacement.ErrNotAnUpgrade},
		{"a fetched file just over 12 s short misses and is refused", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 400, StoredSeconds: 300}, 387.9, replacement.ErrNotAnUpgrade},
		{"a stored file exactly 12 s long fits and keeps the tie", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 400, StoredSeconds: 412}, 400, replacement.ErrNotAnUpgrade},
		{"a stored file exactly 12 s short fits and keeps the tie", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 400, StoredSeconds: 388}, 400, replacement.ErrNotAnUpgrade},
		{"a stored file just over 12 s long misses and is replaced", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 400, StoredSeconds: 412.1}, 400, nil},
		{"a stored file just over 12 s short misses and is replaced", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 400, StoredSeconds: 387.9}, 400, nil},
		{"at a short catalog the 5 s floor applies to the fetched file", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 100, StoredSeconds: 80}, 105, nil},
		{"at a short catalog just past the 5 s floor the fetched file misses", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 100, StoredSeconds: 80}, 105.1, replacement.ErrNotAnUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(audioquality.B, fmt140, tc.seconds)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("AdmitFetched = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGate_AdmitFetchedReplacesAStoredFileLongerThanTheCatalog(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	gate := replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 195, StoredSeconds: 292}
	if err := gate.AdmitFetched(audioquality.B, fmt140, 195); err != nil {
		t.Errorf("fitting fetched file: AdmitFetched = %v, want nil", err)
	}
	if err := gate.AdmitFetched(audioquality.B, fmt140, 292); !errors.Is(err, replacement.ErrNotAnUpgrade) {
		t.Errorf("fetched file as long as the stored one: AdmitFetched = %v, want ErrNotAnUpgrade", err)
	}
}

func TestGate_AdmitFetchedRefusesAFetchedFileLongerThanTheCatalog(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	gate := replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: 231}
	if err := gate.AdmitFetched(audioquality.B, fmt140, 340); !errors.Is(err, replacement.ErrNotAnUpgrade) {
		t.Errorf("AdmitFetched = %v, want ErrNotAnUpgrade", err)
	}
}

func TestGate_AdmitFetchedStillTakesAHigherGradeWhateverTheLength(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	gate := replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: 284}
	if err := gate.AdmitFetched(audioquality.A, fmt140, 120); err != nil {
		t.Errorf("AdmitFetched = %v, want nil", err)
	}
}

func TestGate_AdmitFetchedLengthRescueAlongsideTheLowBitrateRescue(t *testing.T) {
	const floor = 112
	muxed := ports.SourceFormat{ID: "18", Kbps: 96}
	audioOnly := ports.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    replacement.ReplacementGate
		fetched ports.SourceFormat
		seconds float64
		wantErr error
	}{
		{"a muxed stored edit takes a same-grade muxed file that fits the catalog", replacement.ReplacementGate{Current: audioquality.A, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 231}, muxed, 285, nil},
		{"a muxed stored file still takes a better stream that misses the catalog", replacement.ReplacementGate{Current: audioquality.A, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 284}, audioOnly, 231, nil},
		{"a muxed fitting stored file refuses a same-grade muxed file", replacement.ReplacementGate{Current: audioquality.A, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 284}, muxed, 285, replacement.ErrNotAnUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(audioquality.A, tc.fetched, tc.seconds)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("AdmitFetched = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGate_AdmitFetchedLengthRescueIgnoresNegativeAndNaNLengths(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	zero := 0.0
	nan := zero / zero
	cases := []struct {
		name    string
		gate    replacement.ReplacementGate
		seconds float64
	}{
		{"a negative catalog length", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: -285, StoredSeconds: 231}, -285},
		{"a negative stored length", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: -231}, 285},
		{"a negative fetched length", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: 231}, -285},
		{"a NaN catalog length", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: nan, StoredSeconds: 231}, 285},
		{"a NaN stored length", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: nan}, 285},
		{"a NaN fetched length", replacement.ReplacementGate{Current: audioquality.B, CatalogSeconds: 285, StoredSeconds: 231}, nan},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.gate.AdmitFetched(audioquality.B, fmt140, tc.seconds); !errors.Is(err, replacement.ErrNotAnUpgrade) {
				t.Errorf("AdmitFetched = %v, want ErrNotAnUpgrade", err)
			}
		})
	}
}

func TestGate_AdmitFetchedLengthRescueNeedsAKnownCurrentGrade(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	odd := audioquality.Grade("Z")
	gate := replacement.ReplacementGate{Current: odd, CatalogSeconds: 285, StoredSeconds: 231}
	if err := gate.AdmitFetched(odd, fmt140, 285); err == nil {
		t.Error("AdmitFetched = nil, want an unknown grade kept out of the length rescue")
	}
}
