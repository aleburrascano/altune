package upgrade_test

import (
	"altune/go-api/internal/acquisition"
	"errors"
	"testing"
)

func TestGate_AdmitFetched(t *testing.T) {
	const floor = 112
	muxed := acquisition.SourceFormat{ID: "18", Kbps: 96}
	audioOnly := acquisition.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    acquisition.UpgradeGate
		grade   acquisition.Grade
		fetched acquisition.SourceFormat
		dryRun  bool
		wantErr error
	}{
		{"a muxed file at A takes a same-grade audio-only stream", acquisition.UpgradeGate{Current: acquisition.GradeA, Stored: muxed, FloorKbps: floor}, acquisition.GradeA, audioOnly, false, nil},
		{"a muxed file at A refuses a lower-grade audio-only stream", acquisition.UpgradeGate{Current: acquisition.GradeA, Stored: muxed, FloorKbps: floor}, acquisition.GradeB, audioOnly, false, acquisition.ErrNotAnUpgrade},
		{"a muxed file at A refuses a same-grade muxed stream", acquisition.UpgradeGate{Current: acquisition.GradeA, Stored: muxed, FloorKbps: floor}, acquisition.GradeA, muxed, false, acquisition.ErrNotAnUpgrade},
		{"a muxed file at A refuses an audio-only stream under the floor", acquisition.UpgradeGate{Current: acquisition.GradeA, Stored: muxed, FloorKbps: floor}, acquisition.GradeA, acquisition.SourceFormat{ID: "139", Kbps: 48}, false, acquisition.ErrNotAnUpgrade},
		{"an audio-only file at A takes no same-grade stream", acquisition.UpgradeGate{Current: acquisition.GradeA}, acquisition.GradeA, audioOnly, false, acquisition.ErrNotAnUpgrade},
		{"a muxed file at B still takes a higher grade", acquisition.UpgradeGate{Current: acquisition.GradeB, Stored: muxed, FloorKbps: floor}, acquisition.GradeA, muxed, false, nil},
		{"a dry run reports the swap without making it", acquisition.UpgradeGate{Current: acquisition.GradeA, Stored: muxed, FloorKbps: floor, DryRun: true}, acquisition.GradeA, audioOnly, true, acquisition.ErrWouldUpgrade},
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
	fmt140 := acquisition.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    acquisition.UpgradeGate
		grade   acquisition.Grade
		seconds float64
		wantErr error
	}{
		{"a file fitting the catalog replaces a missing stored edit", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285, StoredSeconds: 231}, acquisition.GradeB, 285, nil},
		{"a dry run reports it", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285, StoredSeconds: 231, DryRun: true}, acquisition.GradeB, 285, acquisition.ErrWouldUpgrade},
		{"a fitting stored file keeps the tie, fetched fitting", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285, StoredSeconds: 284}, acquisition.GradeB, 285, acquisition.ErrNotAnUpgrade},
		{"a fitting stored file keeps the tie, fetched missing", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285, StoredSeconds: 284}, acquisition.GradeB, 231, acquisition.ErrNotAnUpgrade},
		{"a fetched file that also misses is refused", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285, StoredSeconds: 231}, acquisition.GradeB, 231, acquisition.ErrNotAnUpgrade},
		{"a lower grade is refused", acquisition.UpgradeGate{Current: acquisition.GradeA, CatalogSeconds: 285, StoredSeconds: 231}, acquisition.GradeB, 285, acquisition.ErrNotAnUpgrade},
		{"an unknown catalog length is refused", acquisition.UpgradeGate{Current: acquisition.GradeB, StoredSeconds: 231}, acquisition.GradeB, 285, acquisition.ErrNotAnUpgrade},
		{"an unknown stored length is refused", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285}, acquisition.GradeB, 285, acquisition.ErrNotAnUpgrade},
		{"an unknown fetched length is refused", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285, StoredSeconds: 231}, acquisition.GradeB, 0, acquisition.ErrNotAnUpgrade},
		{"a zero catalog length is refused even beside a tiny fetched length", acquisition.UpgradeGate{Current: acquisition.GradeB, StoredSeconds: 231}, acquisition.GradeB, 3, acquisition.ErrNotAnUpgrade},
		{"a zero fetched length is refused even beside a tiny catalog length", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 4, StoredSeconds: 231}, acquisition.GradeB, 0, acquisition.ErrNotAnUpgrade},
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
	muxed := acquisition.SourceFormat{ID: "18", Kbps: 96}
	if !(acquisition.UpgradeGate{Current: acquisition.GradeA, Stored: muxed, FloorKbps: 112}).RefetchesCurrentSource() {
		t.Error("a muxed file must be refetched from its own source")
	}
	if (acquisition.UpgradeGate{Current: acquisition.GradeA, Stored: acquisition.SourceFormat{ID: "140", Kbps: 130}, FloorKbps: 112}).RefetchesCurrentSource() {
		t.Error("an audio-only file above the floor must not be refetched")
	}
	if (acquisition.UpgradeGate{Current: acquisition.GradeA, Stored: muxed}).RefetchesCurrentSource() {
		t.Error("a zero floor switches the rescue off")
	}
}

func TestGate_AdmitFetchedByCatalogLengthAtTheToleranceEdges(t *testing.T) {
	fmt140 := acquisition.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    acquisition.UpgradeGate
		seconds float64
		wantErr error
	}{
		{"a fetched file exactly 12 s long fits and is admitted", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 400, StoredSeconds: 300}, 412, nil},
		{"a fetched file exactly 12 s short fits and is admitted", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 400, StoredSeconds: 300}, 388, nil},
		{"a fetched file just over 12 s long misses and is refused", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 400, StoredSeconds: 300}, 412.1, acquisition.ErrNotAnUpgrade},
		{"a fetched file just over 12 s short misses and is refused", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 400, StoredSeconds: 300}, 387.9, acquisition.ErrNotAnUpgrade},
		{"a stored file exactly 12 s long fits and keeps the tie", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 400, StoredSeconds: 412}, 400, acquisition.ErrNotAnUpgrade},
		{"a stored file exactly 12 s short fits and keeps the tie", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 400, StoredSeconds: 388}, 400, acquisition.ErrNotAnUpgrade},
		{"a stored file just over 12 s long misses and is replaced", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 400, StoredSeconds: 412.1}, 400, nil},
		{"a stored file just over 12 s short misses and is replaced", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 400, StoredSeconds: 387.9}, 400, nil},
		{"at a short catalog the 5 s floor applies to the fetched file", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 100, StoredSeconds: 80}, 105, nil},
		{"at a short catalog just past the 5 s floor the fetched file misses", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 100, StoredSeconds: 80}, 105.1, acquisition.ErrNotAnUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(acquisition.GradeB, fmt140, tc.seconds)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("AdmitFetched = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGate_AdmitFetchedReplacesAStoredFileLongerThanTheCatalog(t *testing.T) {
	fmt140 := acquisition.SourceFormat{ID: "140", Kbps: 130}
	gate := acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 195, StoredSeconds: 292}
	if err := gate.AdmitFetched(acquisition.GradeB, fmt140, 195); err != nil {
		t.Errorf("fitting fetched file: AdmitFetched = %v, want nil", err)
	}
	if err := gate.AdmitFetched(acquisition.GradeB, fmt140, 292); !errors.Is(err, acquisition.ErrNotAnUpgrade) {
		t.Errorf("fetched file as long as the stored one: AdmitFetched = %v, want ErrNotAnUpgrade", err)
	}
}

func TestGate_AdmitFetchedRefusesAFetchedFileLongerThanTheCatalog(t *testing.T) {
	fmt140 := acquisition.SourceFormat{ID: "140", Kbps: 130}
	gate := acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285, StoredSeconds: 231}
	if err := gate.AdmitFetched(acquisition.GradeB, fmt140, 340); !errors.Is(err, acquisition.ErrNotAnUpgrade) {
		t.Errorf("AdmitFetched = %v, want ErrNotAnUpgrade", err)
	}
}

func TestGate_AdmitFetchedStillTakesAHigherGradeWhateverTheLength(t *testing.T) {
	fmt140 := acquisition.SourceFormat{ID: "140", Kbps: 130}
	gate := acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285, StoredSeconds: 284}
	if err := gate.AdmitFetched(acquisition.GradeA, fmt140, 120); err != nil {
		t.Errorf("AdmitFetched = %v, want nil", err)
	}
}

func TestGate_AdmitFetchedLengthRescueAlongsideTheLowBitrateRescue(t *testing.T) {
	const floor = 112
	muxed := acquisition.SourceFormat{ID: "18", Kbps: 96}
	audioOnly := acquisition.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    acquisition.UpgradeGate
		fetched acquisition.SourceFormat
		seconds float64
		wantErr error
	}{
		{"a muxed stored edit takes a same-grade muxed file that fits the catalog", acquisition.UpgradeGate{Current: acquisition.GradeA, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 231}, muxed, 285, nil},
		{"a muxed stored file still takes a better stream that misses the catalog", acquisition.UpgradeGate{Current: acquisition.GradeA, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 284}, audioOnly, 231, nil},
		{"a muxed fitting stored file refuses a same-grade muxed file", acquisition.UpgradeGate{Current: acquisition.GradeA, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 284}, muxed, 285, acquisition.ErrNotAnUpgrade},
		{"a dry run with both rescues holding reports the swap", acquisition.UpgradeGate{Current: acquisition.GradeA, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 231, DryRun: true}, audioOnly, 285, acquisition.ErrWouldUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(acquisition.GradeA, tc.fetched, tc.seconds)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("AdmitFetched = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGate_AdmitFetchedLengthRescueIgnoresNegativeAndNaNLengths(t *testing.T) {
	fmt140 := acquisition.SourceFormat{ID: "140", Kbps: 130}
	zero := 0.0
	nan := zero / zero
	cases := []struct {
		name    string
		gate    acquisition.UpgradeGate
		seconds float64
	}{
		{"a negative catalog length", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: -285, StoredSeconds: 231}, -285},
		{"a negative stored length", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285, StoredSeconds: -231}, 285},
		{"a negative fetched length", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285, StoredSeconds: 231}, -285},
		{"a NaN catalog length", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: nan, StoredSeconds: 231}, 285},
		{"a NaN stored length", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285, StoredSeconds: nan}, 285},
		{"a NaN fetched length", acquisition.UpgradeGate{Current: acquisition.GradeB, CatalogSeconds: 285, StoredSeconds: 231}, nan},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.gate.AdmitFetched(acquisition.GradeB, fmt140, tc.seconds); !errors.Is(err, acquisition.ErrNotAnUpgrade) {
				t.Errorf("AdmitFetched = %v, want ErrNotAnUpgrade", err)
			}
		})
	}
}

func TestGate_AdmitFetchedLengthRescueNeedsAKnownCurrentGrade(t *testing.T) {
	fmt140 := acquisition.SourceFormat{ID: "140", Kbps: 130}
	odd := acquisition.Grade("Z")
	gate := acquisition.UpgradeGate{Current: odd, CatalogSeconds: 285, StoredSeconds: 231}
	if err := gate.AdmitFetched(odd, fmt140, 285); err == nil {
		t.Error("AdmitFetched = nil, want an unknown grade kept out of the length rescue")
	}
}
