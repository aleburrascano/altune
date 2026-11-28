package service_test

import (
	"altune/go-api/internal/download/downloadtest"
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/download/internal/service"
	"altune/go-api/internal/shared/music"
	"math"
	"testing"
)

func contextWithLengths(saved, resolved float64) *service.AcquisitionContext {
	return &service.AcquisitionContext{
		Track:    service.TrackRef{CatalogDuration: saved},
		Identity: domain.RecordingIdentity{Duration: resolved},
	}
}

func TestProvenance_DisagreementIsNeverCorroborated(t *testing.T) {
	ac := contextWithLengths(232, 276)
	ac.DurationVerified = true

	if got := ac.Provenance(); got != music.ProvenanceBestEffort {
		t.Errorf("Provenance() = %q, want best_effort — a length that passed a window it disagrees with proves nothing", got)
	}
}

func TestProvenance_AVerifiedAgreeingLengthIsCorroborated(t *testing.T) {
	ac := contextWithLengths(262, 263)
	ac.DurationVerified = true

	if got := ac.Provenance(); got != music.ProvenanceCorroborated {
		t.Errorf("Provenance() = %q, want corroborated when the saved and resolved lengths agree", got)
	}
}

func TestProvenance_OneLengthSourceIsNotCorroborated(t *testing.T) {
	ac := contextWithLengths(0, 181)
	ac.DurationVerified = true

	if got := ac.Provenance(); got != music.ProvenanceBestEffort {
		t.Errorf("Provenance() = %q, want best_effort - the MB length alone checked against itself proves nothing", got)
	}
}

func lengthOfSeconds(t *testing.T, seconds float64) music.TrackLength {
	t.Helper()
	length, err := music.TrackLengthOf(seconds)
	if err != nil {
		t.Fatalf("TrackLengthOf(%v) = %v", seconds, err)
	}
	return length
}

func TestStoredLength_PrefersTheProbedLengthOverTheSourcesReportedOne(t *testing.T) {
	ac := &service.AcquisitionContext{
		ProbedDuration: lengthOfSeconds(t, 232),
		Selected:       downloadtest.CandidatePtr("", downloadtest.WithDuration(999)),
	}
	if got := ac.StoredLength().Seconds(); got != 232 {
		t.Errorf("StoredLength() = %v s, want the probed 232 rather than the source's metadata", got)
	}
}

func TestStoredLength_FallsBackToTheChosenSourcesReportedLength(t *testing.T) {
	ac := &service.AcquisitionContext{Selected: downloadtest.CandidatePtr("", downloadtest.WithDuration(240))}
	if got := ac.StoredLength().Seconds(); got != 240 {
		t.Errorf("StoredLength() = %v s, want the source's 240 when nothing was probed", got)
	}
}

func TestStoredLength_IsUnknownWhenTheSourceReportsALengthNoRecordingCanHave(t *testing.T) {
	for name, reported := range map[string]float64{"above seven days": 8 * 24 * 60 * 60, "negative": -1, "not a number": math.NaN()} {
		ac := &service.AcquisitionContext{Selected: downloadtest.CandidatePtr("", downloadtest.WithDuration(reported))}
		if got := ac.StoredLength(); got.Known() {
			t.Errorf("%s: StoredLength() = %v s, want an unknown length", name, got.Seconds())
		}
	}
	if got := (&service.AcquisitionContext{}).StoredLength(); got.Known() {
		t.Errorf("no probe and no selected source: StoredLength() = %v s, want unknown", got.Seconds())
	}
}

func TestExpectedLength_OneLengthForEveryReader(t *testing.T) {
	cases := []struct {
		name string
		ac   *service.AcquisitionContext
		want float64
	}{
		{"catalog length wins over a trusted resolved one", &service.AcquisitionContext{
			Track: service.TrackRef{CatalogDuration: 139}, Identity: domain.RecordingIdentity{Duration: 167},
		}, 139},
		{"resolved length when no target length is saved", contextWithLengths(0, 140), 140},
		{"target length when the resolved one is doubted", &service.AcquisitionContext{
			Track: service.TrackRef{CatalogDuration: 139}, Identity: domain.RecordingIdentity{Duration: 167, ReferenceDoubted: true},
		}, 139},
		{"target length when nothing was resolved", contextWithLengths(139, 0), 139},
		{"no length when the only resolved one is doubted", &service.AcquisitionContext{
			Identity: domain.RecordingIdentity{Duration: 167, ReferenceDoubted: true},
		}, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ac.ExpectedLength(); got != tc.want {
				t.Errorf("ExpectedLength = %v, want %v", got, tc.want)
			}
		})
	}
}
