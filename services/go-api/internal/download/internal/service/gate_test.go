package service_test

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/ports"
	"altune/go-api/internal/download/internal/service"
	"errors"
	"testing"
)

func TestGate_AdmitFetched(t *testing.T) {
	const floor = 112
	muxed := ports.SourceFormat{ID: "18", Kbps: 96}
	audioOnly := ports.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    service.ReplacementGate
		grade   domain.Grade
		fetched ports.SourceFormat
		wantErr error
	}{
		{"a muxed file at A takes a same-grade audio-only stream", service.ReplacementGate{Current: domain.A, Stored: muxed, FloorKbps: floor}, domain.A, audioOnly, nil},
		{"a muxed file at A refuses a lower-grade audio-only stream", service.ReplacementGate{Current: domain.A, Stored: muxed, FloorKbps: floor}, domain.B, audioOnly, service.ErrNotAnUpgrade},
		{"a muxed file at A refuses a same-grade muxed stream", service.ReplacementGate{Current: domain.A, Stored: muxed, FloorKbps: floor}, domain.A, muxed, service.ErrNotAnUpgrade},
		{"a muxed file at A refuses an audio-only stream under the floor", service.ReplacementGate{Current: domain.A, Stored: muxed, FloorKbps: floor}, domain.A, ports.SourceFormat{ID: "139", Kbps: 48}, service.ErrNotAnUpgrade},
		{"an audio-only file at A takes no same-grade stream", service.ReplacementGate{Current: domain.A}, domain.A, audioOnly, service.ErrNotAnUpgrade},
		{"a muxed file at B still takes a higher grade", service.ReplacementGate{Current: domain.B, Stored: muxed, FloorKbps: floor}, domain.A, muxed, nil},
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
		gate    service.ReplacementGate
		grade   domain.Grade
		seconds float64
		wantErr error
	}{
		{"a file fitting the catalog replaces a missing stored edit", service.ReplacementGate{Current: domain.B, CatalogSeconds: 285, StoredSeconds: 231}, domain.B, 285, nil},
		{"a fitting stored file keeps the tie, fetched fitting", service.ReplacementGate{Current: domain.B, CatalogSeconds: 285, StoredSeconds: 284}, domain.B, 285, service.ErrNotAnUpgrade},
		{"a fitting stored file keeps the tie, fetched missing", service.ReplacementGate{Current: domain.B, CatalogSeconds: 285, StoredSeconds: 284}, domain.B, 231, service.ErrNotAnUpgrade},
		{"a fetched file that also misses is refused", service.ReplacementGate{Current: domain.B, CatalogSeconds: 285, StoredSeconds: 231}, domain.B, 231, service.ErrNotAnUpgrade},
		{"a lower grade is refused", service.ReplacementGate{Current: domain.A, CatalogSeconds: 285, StoredSeconds: 231}, domain.B, 285, service.ErrNotAnUpgrade},
		{"an unknown catalog length is refused", service.ReplacementGate{Current: domain.B, StoredSeconds: 231}, domain.B, 285, service.ErrNotAnUpgrade},
		{"an unknown stored length is refused", service.ReplacementGate{Current: domain.B, CatalogSeconds: 285}, domain.B, 285, service.ErrNotAnUpgrade},
		{"an unknown fetched length is refused", service.ReplacementGate{Current: domain.B, CatalogSeconds: 285, StoredSeconds: 231}, domain.B, 0, service.ErrNotAnUpgrade},
		{"a zero catalog length is refused even beside a tiny fetched length", service.ReplacementGate{Current: domain.B, StoredSeconds: 231}, domain.B, 3, service.ErrNotAnUpgrade},
		{"a zero fetched length is refused even beside a tiny catalog length", service.ReplacementGate{Current: domain.B, CatalogSeconds: 4, StoredSeconds: 231}, domain.B, 0, service.ErrNotAnUpgrade},
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
	if !(service.ReplacementGate{Current: domain.A, Stored: muxed, FloorKbps: 112}).RefetchesCurrentSource() {
		t.Error("a muxed file must be refetched from its own source")
	}
	if (service.ReplacementGate{Current: domain.A, Stored: ports.SourceFormat{ID: "140", Kbps: 130}, FloorKbps: 112}).RefetchesCurrentSource() {
		t.Error("an audio-only file above the floor must not be refetched")
	}
	if (service.ReplacementGate{Current: domain.A, Stored: muxed}).RefetchesCurrentSource() {
		t.Error("a zero floor switches the rescue off")
	}
}

func TestGate_AdmitFetchedByCatalogLengthAtTheToleranceEdges(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    service.ReplacementGate
		seconds float64
		wantErr error
	}{
		{"a fetched file exactly 12 s long fits and is admitted", service.ReplacementGate{Current: domain.B, CatalogSeconds: 400, StoredSeconds: 300}, 412, nil},
		{"a fetched file exactly 12 s short fits and is admitted", service.ReplacementGate{Current: domain.B, CatalogSeconds: 400, StoredSeconds: 300}, 388, nil},
		{"a fetched file just over 12 s long misses and is refused", service.ReplacementGate{Current: domain.B, CatalogSeconds: 400, StoredSeconds: 300}, 412.1, service.ErrNotAnUpgrade},
		{"a fetched file just over 12 s short misses and is refused", service.ReplacementGate{Current: domain.B, CatalogSeconds: 400, StoredSeconds: 300}, 387.9, service.ErrNotAnUpgrade},
		{"a stored file exactly 12 s long fits and keeps the tie", service.ReplacementGate{Current: domain.B, CatalogSeconds: 400, StoredSeconds: 412}, 400, service.ErrNotAnUpgrade},
		{"a stored file exactly 12 s short fits and keeps the tie", service.ReplacementGate{Current: domain.B, CatalogSeconds: 400, StoredSeconds: 388}, 400, service.ErrNotAnUpgrade},
		{"a stored file just over 12 s long misses and is replaced", service.ReplacementGate{Current: domain.B, CatalogSeconds: 400, StoredSeconds: 412.1}, 400, nil},
		{"a stored file just over 12 s short misses and is replaced", service.ReplacementGate{Current: domain.B, CatalogSeconds: 400, StoredSeconds: 387.9}, 400, nil},
		{"at a short catalog the 5 s floor applies to the fetched file", service.ReplacementGate{Current: domain.B, CatalogSeconds: 100, StoredSeconds: 80}, 105, nil},
		{"at a short catalog just past the 5 s floor the fetched file misses", service.ReplacementGate{Current: domain.B, CatalogSeconds: 100, StoredSeconds: 80}, 105.1, service.ErrNotAnUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(domain.B, fmt140, tc.seconds)
			if !errors.Is(err, tc.wantErr) || (tc.wantErr == nil && err != nil) {
				t.Errorf("AdmitFetched = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestGate_AdmitFetchedReplacesAStoredFileLongerThanTheCatalog(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	gate := service.ReplacementGate{Current: domain.B, CatalogSeconds: 195, StoredSeconds: 292}
	if err := gate.AdmitFetched(domain.B, fmt140, 195); err != nil {
		t.Errorf("fitting fetched file: AdmitFetched = %v, want nil", err)
	}
	if err := gate.AdmitFetched(domain.B, fmt140, 292); !errors.Is(err, service.ErrNotAnUpgrade) {
		t.Errorf("fetched file as long as the stored one: AdmitFetched = %v, want ErrNotAnUpgrade", err)
	}
}

func TestGate_AdmitFetchedRefusesAFetchedFileLongerThanTheCatalog(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	gate := service.ReplacementGate{Current: domain.B, CatalogSeconds: 285, StoredSeconds: 231}
	if err := gate.AdmitFetched(domain.B, fmt140, 340); !errors.Is(err, service.ErrNotAnUpgrade) {
		t.Errorf("AdmitFetched = %v, want ErrNotAnUpgrade", err)
	}
}

func TestGate_AdmitFetchedStillTakesAHigherGradeWhateverTheLength(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	gate := service.ReplacementGate{Current: domain.B, CatalogSeconds: 285, StoredSeconds: 284}
	if err := gate.AdmitFetched(domain.A, fmt140, 120); err != nil {
		t.Errorf("AdmitFetched = %v, want nil", err)
	}
}

func TestGate_AdmitFetchedLengthRescueAlongsideTheLowBitrateRescue(t *testing.T) {
	const floor = 112
	muxed := ports.SourceFormat{ID: "18", Kbps: 96}
	audioOnly := ports.SourceFormat{ID: "140", Kbps: 130}
	cases := []struct {
		name    string
		gate    service.ReplacementGate
		fetched ports.SourceFormat
		seconds float64
		wantErr error
	}{
		{"a muxed stored edit takes a same-grade muxed file that fits the catalog", service.ReplacementGate{Current: domain.A, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 231}, muxed, 285, nil},
		{"a muxed stored file still takes a better stream that misses the catalog", service.ReplacementGate{Current: domain.A, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 284}, audioOnly, 231, nil},
		{"a muxed fitting stored file refuses a same-grade muxed file", service.ReplacementGate{Current: domain.A, Stored: muxed, FloorKbps: floor, CatalogSeconds: 285, StoredSeconds: 284}, muxed, 285, service.ErrNotAnUpgrade},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.gate.AdmitFetched(domain.A, tc.fetched, tc.seconds)
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
		gate    service.ReplacementGate
		seconds float64
	}{
		{"a negative catalog length", service.ReplacementGate{Current: domain.B, CatalogSeconds: -285, StoredSeconds: 231}, -285},
		{"a negative stored length", service.ReplacementGate{Current: domain.B, CatalogSeconds: 285, StoredSeconds: -231}, 285},
		{"a negative fetched length", service.ReplacementGate{Current: domain.B, CatalogSeconds: 285, StoredSeconds: 231}, -285},
		{"a NaN catalog length", service.ReplacementGate{Current: domain.B, CatalogSeconds: nan, StoredSeconds: 231}, 285},
		{"a NaN stored length", service.ReplacementGate{Current: domain.B, CatalogSeconds: 285, StoredSeconds: nan}, 285},
		{"a NaN fetched length", service.ReplacementGate{Current: domain.B, CatalogSeconds: 285, StoredSeconds: 231}, nan},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.gate.AdmitFetched(domain.B, fmt140, tc.seconds); !errors.Is(err, service.ErrNotAnUpgrade) {
				t.Errorf("AdmitFetched = %v, want ErrNotAnUpgrade", err)
			}
		})
	}
}

func TestGate_AdmitFetchedLengthRescueNeedsAKnownCurrentGrade(t *testing.T) {
	fmt140 := ports.SourceFormat{ID: "140", Kbps: 130}
	odd := domain.Grade("Z")
	gate := service.ReplacementGate{Current: odd, CatalogSeconds: 285, StoredSeconds: 231}
	if err := gate.AdmitFetched(odd, fmt140, 285); err == nil {
		t.Error("AdmitFetched = nil, want an unknown grade kept out of the length rescue")
	}
}
