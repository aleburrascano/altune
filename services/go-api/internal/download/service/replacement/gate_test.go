package replacement_test

import (
	"altune/go-api/internal/download"
	"errors"
	"testing"
)

func TestGate_AdmitFetched(t *testing.T) {
	const floor = 112
	muxed := download.SourceFormat{ID: "18", Kbps: 96}
	audioOnly := download.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    download.ReplacementGate
		grade   download.Grade
		fetched download.SourceFormat
		wantErr error
	}{
		{"a muxed file at A takes a same-grade audio-only stream", download.ReplacementGate{Current: download.GradeA, Stored: muxed, FloorKbps: floor}, download.GradeA, audioOnly, nil},
		{"a muxed file at A refuses a lower-grade audio-only stream", download.ReplacementGate{Current: download.GradeA, Stored: muxed, FloorKbps: floor}, download.GradeB, audioOnly, download.ErrNotAnUpgrade},
		{"a muxed file at A refuses a same-grade muxed stream", download.ReplacementGate{Current: download.GradeA, Stored: muxed, FloorKbps: floor}, download.GradeA, muxed, download.ErrNotAnUpgrade},
		{"a muxed file at A refuses an audio-only stream under the floor", download.ReplacementGate{Current: download.GradeA, Stored: muxed, FloorKbps: floor}, download.GradeA, download.SourceFormat{ID: "139", Kbps: 48}, download.ErrNotAnUpgrade},
		{"an audio-only file at A takes no same-grade stream", download.ReplacementGate{Current: download.GradeA}, download.GradeA, audioOnly, download.ErrNotAnUpgrade},
		{"a muxed file at B still takes a higher grade", download.ReplacementGate{Current: download.GradeB, Stored: muxed, FloorKbps: floor}, download.GradeA, muxed, nil},
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
	fmt140 := download.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    download.ReplacementGate
		grade   download.Grade
		seconds float64
		wantErr error
	}{
		{"a file fitting the catalog replaces a missing stored edit", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 285, StoredSeconds: 231}, download.GradeB, 285, nil},
		{"a fitting stored file keeps the tie, fetched fitting", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 285, StoredSeconds: 284}, download.GradeB, 285, download.ErrNotAnUpgrade},
		{"a fitting stored file keeps the tie, fetched missing", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 285, StoredSeconds: 284}, download.GradeB, 231, download.ErrNotAnUpgrade},
		{"a fetched file that also misses is refused", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 285, StoredSeconds: 231}, download.GradeB, 231, download.ErrNotAnUpgrade},
		{"a lower grade is refused", download.ReplacementGate{Current: download.GradeA, CatalogSeconds: 285, StoredSeconds: 231}, download.GradeB, 285, download.ErrNotAnUpgrade},
		{"an unknown catalog length is refused", download.ReplacementGate{Current: download.GradeB, StoredSeconds: 231}, download.GradeB, 285, download.ErrNotAnUpgrade},
		{"an unknown stored length is refused", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 285}, download.GradeB, 285, download.ErrNotAnUpgrade},
		{"an unknown fetched length is refused", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 285, StoredSeconds: 231}, download.GradeB, 0, download.ErrNotAnUpgrade},
		{"a zero catalog length is refused even beside a tiny fetched length", download.ReplacementGate{Current: download.GradeB, StoredSeconds: 231}, download.GradeB, 3, download.ErrNotAnUpgrade},
		{"a zero fetched length is refused even beside a tiny catalog length", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 4, StoredSeconds: 231}, download.GradeB, 0, download.ErrNotAnUpgrade},
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
	muxed := download.SourceFormat{ID: "18", Kbps: 96}
	if !(download.ReplacementGate{Current: download.GradeA, Stored: muxed, FloorKbps: 112}).RefetchesCurrentSource() {
		t.Error("a muxed file must be refetched from its own source")
	}
	if (download.ReplacementGate{Current: download.GradeA, Stored: download.SourceFormat{ID: "140", Kbps: 130}, FloorKbps: 112}).RefetchesCurrentSource() {
		t.Error("an audio-only file above the floor must not be refetched")
	}
	if (download.ReplacementGate{Current: download.GradeA, Stored: muxed}).RefetchesCurrentSource() {
		t.Error("a zero floor switches the rescue off")
	}
}

func TestGate_AdmitFetchedByCatalogLengthAtTheToleranceEdges(t *testing.T) {
	fmt140 := download.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    download.ReplacementGate
		seconds float64
		wantErr error
	}{
		{"a fetched file exactly 12 s long fits and is admitted", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 400, StoredSeconds: 300}, 412, nil},
		{"a fetched file exactly 12 s short fits and is admitted", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 400, StoredSeconds: 300}, 388, nil},
		{"a fetched file just over 12 s long misses and is refused", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 400, StoredSeconds: 300}, 412.1, download.ErrNotAnUpgrade},
		{"a fetched file just over 12 s short misses and is refused", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 400, StoredSeconds: 300}, 387.9, download.ErrNotAnUpgrade},
		{"a stored file exactly 12 s long fits and keeps the tie", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 400, StoredSeconds: 412}, 400, download.ErrNotAnUpgrade},
		{"a stored file exactly 12 s short fits and keeps the tie", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 400, StoredSeconds: 388}, 400, download.ErrNotAnUpgrade},
		{"a stored file just over 12 s long misses and is replaced", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 400, StoredSeconds: 412.1}, 400, nil},
		{"a stored file just over 12 s short misses and is replaced", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 400, StoredSeconds: 387.9}, 400, nil},
		{"at a short catalog the 5 s floor applies to the fetched file", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 100, StoredSeconds: 80}, 105, nil},
		{"at a short catalog just past the 5 s floor the fetched file misses", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 100, StoredSeconds: 80}, 105.1, download.ErrNotAnUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(download.GradeB, fmt140, tc.seconds)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("AdmitFetched = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGate_AdmitFetchedReplacesAStoredFileLongerThanTheCatalog(t *testing.T) {
	fmt140 := download.SourceFormat{ID: "140", Kbps: 130}
	gate := download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 195, StoredSeconds: 292}
	if err := gate.AdmitFetched(download.GradeB, fmt140, 195); err != nil {
		t.Errorf("fitting fetched file: AdmitFetched = %v, want nil", err)
	}
	if err := gate.AdmitFetched(download.GradeB, fmt140, 292); !errors.Is(err, download.ErrNotAnUpgrade) {
		t.Errorf("fetched file as long as the stored one: AdmitFetched = %v, want ErrNotAnUpgrade", err)
	}
}

func TestGate_AdmitFetchedRefusesAFetchedFileLongerThanTheCatalog(t *testing.T) {
	fmt140 := download.SourceFormat{ID: "140", Kbps: 130}
	gate := download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 285, StoredSeconds: 231}
	if err := gate.AdmitFetched(download.GradeB, fmt140, 340); !errors.Is(err, download.ErrNotAnUpgrade) {
		t.Errorf("AdmitFetched = %v, want ErrNotAnUpgrade", err)
	}
}

func TestGate_AdmitFetchedStillTakesAHigherGradeWhateverTheLength(t *testing.T) {
	fmt140 := download.SourceFormat{ID: "140", Kbps: 130}
	gate := download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 285, StoredSeconds: 284}
	if err := gate.AdmitFetched(download.GradeA, fmt140, 120); err != nil {
		t.Errorf("AdmitFetched = %v, want nil", err)
	}
}

func TestGate_AdmitFetchedLengthRescueAlongsideTheLowBitrateRescue(t *testing.T) {
	const floor = 112
	muxed := download.SourceFormat{ID: "18", Kbps: 96}
	audioOnly := download.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    download.ReplacementGate
		fetched download.SourceFormat
		seconds float64
		wantErr error
	}{
		{"a muxed stored edit takes a same-grade muxed file that fits the catalog", download.ReplacementGate{Current: download.GradeA, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 231}, muxed, 285, nil},
		{"a muxed stored file still takes a better stream that misses the catalog", download.ReplacementGate{Current: download.GradeA, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 284}, audioOnly, 231, nil},
		{"a muxed fitting stored file refuses a same-grade muxed file", download.ReplacementGate{Current: download.GradeA, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 284}, muxed, 285, download.ErrNotAnUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(download.GradeA, tc.fetched, tc.seconds)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("AdmitFetched = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGate_AdmitFetchedLengthRescueIgnoresNegativeAndNaNLengths(t *testing.T) {
	fmt140 := download.SourceFormat{ID: "140", Kbps: 130}
	zero := 0.0
	nan := zero / zero
	cases := []struct {
		name    string
		gate    download.ReplacementGate
		seconds float64
	}{
		{"a negative catalog length", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: -285, StoredSeconds: 231}, -285},
		{"a negative stored length", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 285, StoredSeconds: -231}, 285},
		{"a negative fetched length", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 285, StoredSeconds: 231}, -285},
		{"a NaN catalog length", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: nan, StoredSeconds: 231}, 285},
		{"a NaN stored length", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 285, StoredSeconds: nan}, 285},
		{"a NaN fetched length", download.ReplacementGate{Current: download.GradeB, CatalogSeconds: 285, StoredSeconds: 231}, nan},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.gate.AdmitFetched(download.GradeB, fmt140, tc.seconds); !errors.Is(err, download.ErrNotAnUpgrade) {
				t.Errorf("AdmitFetched = %v, want ErrNotAnUpgrade", err)
			}
		})
	}
}

func TestGate_AdmitFetchedLengthRescueNeedsAKnownCurrentGrade(t *testing.T) {
	fmt140 := download.SourceFormat{ID: "140", Kbps: 130}
	odd := download.Grade("Z")
	gate := download.ReplacementGate{Current: odd, CatalogSeconds: 285, StoredSeconds: 231}
	if err := gate.AdmitFetched(odd, fmt140, 285); err == nil {
		t.Error("AdmitFetched = nil, want an unknown grade kept out of the length rescue")
	}
}
