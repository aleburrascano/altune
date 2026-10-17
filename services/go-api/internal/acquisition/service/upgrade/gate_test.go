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
		gate    Gate
		grade   grade.Grade
		seconds float64
		wantErr error
	}{
		{"a file fitting the catalog replaces a missing stored edit", Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 231}, grade.B, 285, nil},
		{"a dry run reports it", Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 231, DryRun: true}, grade.B, 285, ErrWouldUpgrade},
		{"a fitting stored file keeps the tie, fetched fitting", Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 284}, grade.B, 285, ErrNotAnUpgrade},
		{"a fitting stored file keeps the tie, fetched missing", Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 284}, grade.B, 231, ErrNotAnUpgrade},
		{"a fetched file that also misses is refused", Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 231}, grade.B, 231, ErrNotAnUpgrade},
		{"a lower grade is refused", Gate{Current: grade.A, CatalogSeconds: 285, StoredSeconds: 231}, grade.B, 285, ErrNotAnUpgrade},
		{"an unknown catalog length is refused", Gate{Current: grade.B, StoredSeconds: 231}, grade.B, 285, ErrNotAnUpgrade},
		{"an unknown stored length is refused", Gate{Current: grade.B, CatalogSeconds: 285}, grade.B, 285, ErrNotAnUpgrade},
		{"an unknown fetched length is refused", Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 231}, grade.B, 0, ErrNotAnUpgrade},
		{"a zero catalog length is refused even beside a tiny fetched length", Gate{Current: grade.B, StoredSeconds: 231}, grade.B, 3, ErrNotAnUpgrade},
		{"a zero fetched length is refused even beside a tiny catalog length", Gate{Current: grade.B, CatalogSeconds: 4, StoredSeconds: 231}, grade.B, 0, ErrNotAnUpgrade},
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

func TestGate_AdmitFetchedByCatalogLengthAtTheToleranceEdges(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    Gate
		seconds float64
		wantErr error
	}{
		{"a fetched file exactly 12 s long fits and is admitted", Gate{Current: grade.B, CatalogSeconds: 400, StoredSeconds: 300}, 412, nil},
		{"a fetched file exactly 12 s short fits and is admitted", Gate{Current: grade.B, CatalogSeconds: 400, StoredSeconds: 300}, 388, nil},
		{"a fetched file just over 12 s long misses and is refused", Gate{Current: grade.B, CatalogSeconds: 400, StoredSeconds: 300}, 412.1, ErrNotAnUpgrade},
		{"a fetched file just over 12 s short misses and is refused", Gate{Current: grade.B, CatalogSeconds: 400, StoredSeconds: 300}, 387.9, ErrNotAnUpgrade},
		{"a stored file exactly 12 s long fits and keeps the tie", Gate{Current: grade.B, CatalogSeconds: 400, StoredSeconds: 412}, 400, ErrNotAnUpgrade},
		{"a stored file exactly 12 s short fits and keeps the tie", Gate{Current: grade.B, CatalogSeconds: 400, StoredSeconds: 388}, 400, ErrNotAnUpgrade},
		{"a stored file just over 12 s long misses and is replaced", Gate{Current: grade.B, CatalogSeconds: 400, StoredSeconds: 412.1}, 400, nil},
		{"a stored file just over 12 s short misses and is replaced", Gate{Current: grade.B, CatalogSeconds: 400, StoredSeconds: 387.9}, 400, nil},
		{"at a short catalog the 5 s floor applies to the fetched file", Gate{Current: grade.B, CatalogSeconds: 100, StoredSeconds: 80}, 105, nil},
		{"at a short catalog just past the 5 s floor the fetched file misses", Gate{Current: grade.B, CatalogSeconds: 100, StoredSeconds: 80}, 105.1, ErrNotAnUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(grade.B, fmt140, tc.seconds)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("AdmitFetched = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGate_AdmitFetchedReplacesAStoredFileLongerThanTheCatalog(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	gate := Gate{Current: grade.B, CatalogSeconds: 195, StoredSeconds: 292}
	if err := gate.AdmitFetched(grade.B, fmt140, 195); err != nil {
		t.Errorf("fitting fetched file: AdmitFetched = %v, want nil", err)
	}
	if err := gate.AdmitFetched(grade.B, fmt140, 292); !errors.Is(err, ErrNotAnUpgrade) {
		t.Errorf("fetched file as long as the stored one: AdmitFetched = %v, want ErrNotAnUpgrade", err)
	}
}

func TestGate_AdmitFetchedRefusesAFetchedFileLongerThanTheCatalog(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	gate := Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 231}
	if err := gate.AdmitFetched(grade.B, fmt140, 340); !errors.Is(err, ErrNotAnUpgrade) {
		t.Errorf("AdmitFetched = %v, want ErrNotAnUpgrade", err)
	}
}

func TestGate_AdmitFetchedStillTakesAHigherGradeWhateverTheLength(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	gate := Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 284}
	if err := gate.AdmitFetched(grade.A, fmt140, 120); err != nil {
		t.Errorf("AdmitFetched = %v, want nil", err)
	}
}

func TestGate_AdmitFetchedLengthRescueAlongsideTheLowBitrateRescue(t *testing.T) {
	const floor = 112
	muxed := ports.SourceFormat{ID: "18", Kbps: 96}
	audioOnly := ports.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    Gate
		fetched ports.SourceFormat
		seconds float64
		wantErr error
	}{
		{"a muxed stored edit takes a same-grade muxed file that fits the catalog", Gate{Current: grade.A, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 231}, muxed, 285, nil},
		{"a muxed stored file still takes a better stream that misses the catalog", Gate{Current: grade.A, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 284}, audioOnly, 231, nil},
		{"a muxed fitting stored file refuses a same-grade muxed file", Gate{Current: grade.A, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 284}, muxed, 285, ErrNotAnUpgrade},
		{"a dry run with both rescues holding reports the swap", Gate{Current: grade.A, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 231, DryRun: true}, audioOnly, 285, ErrWouldUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(grade.A, tc.fetched, tc.seconds)
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
		gate    Gate
		seconds float64
	}{
		{"a negative catalog length", Gate{Current: grade.B, CatalogSeconds: -285, StoredSeconds: 231}, -285},
		{"a negative stored length", Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: -231}, 285},
		{"a negative fetched length", Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 231}, -285},
		{"a NaN catalog length", Gate{Current: grade.B, CatalogSeconds: nan, StoredSeconds: 231}, 285},
		{"a NaN stored length", Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: nan}, 285},
		{"a NaN fetched length", Gate{Current: grade.B, CatalogSeconds: 285, StoredSeconds: 231}, nan},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.gate.AdmitFetched(grade.B, fmt140, tc.seconds); !errors.Is(err, ErrNotAnUpgrade) {
				t.Errorf("AdmitFetched = %v, want ErrNotAnUpgrade", err)
			}
		})
	}
}

func TestGate_AdmitFetchedLengthRescueNeedsAKnownCurrentGrade(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	odd := grade.Grade("Z")
	gate := Gate{Current: odd, CatalogSeconds: 285, StoredSeconds: 231}
	if err := gate.AdmitFetched(odd, fmt140, 285); err == nil {
		t.Error("AdmitFetched = nil, want an unknown grade kept out of the length rescue")
	}
}
