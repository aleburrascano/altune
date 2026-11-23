package service_test

import (
	"altune/go-api/internal/acquisition/internal/core/ports"
	"altune/go-api/internal/acquisition/service"
	shared "altune/go-api/internal/shared/kernel"
	"testing"
)

func contextWithLengths(saved, resolved float64) *service.AcquisitionContext {
	return &service.AcquisitionContext{
		Track:    service.TrackRef{CatalogDuration: saved},
		Identity: ports.RecordingIdentity{Duration: resolved},
	}
}

func TestProvenance_DisagreementIsNeverCorroborated(t *testing.T) {
	ac := contextWithLengths(232, 276)
	ac.DurationVerified = true

	if got := ac.Provenance(); got != shared.ProvenanceBestEffort {
		t.Errorf("Provenance() = %q, want best_effort — a length that passed a window it disagrees with proves nothing", got)
	}
}

func TestProvenance_AVerifiedAgreeingLengthIsCorroborated(t *testing.T) {
	ac := contextWithLengths(262, 263)
	ac.DurationVerified = true

	if got := ac.Provenance(); got != shared.ProvenanceCorroborated {
		t.Errorf("Provenance() = %q, want corroborated when the saved and resolved lengths agree", got)
	}
}

func TestProvenance_OneLengthSourceIsNotCorroborated(t *testing.T) {
	ac := contextWithLengths(0, 181)
	ac.DurationVerified = true

	if got := ac.Provenance(); got != shared.ProvenanceBestEffort {
		t.Errorf("Provenance() = %q, want best_effort - the MB length alone checked against itself proves nothing", got)
	}
}

func TestMeasuredDuration_PrefersTheProbedValue(t *testing.T) {
	ac := &service.AcquisitionContext{
		ProbedDuration: 232,
		Selected:       &ports.AudioCandidate{Duration: 999},
	}
	if got := ac.MeasuredDuration(); got != 232 {
		t.Errorf("MeasuredDuration() = %v, want the probed 232 rather than provider metadata", got)
	}

	ac = &service.AcquisitionContext{Selected: &ports.AudioCandidate{Duration: 240}}
	if got := ac.MeasuredDuration(); got != 240 {
		t.Errorf("MeasuredDuration() = %v, want the provider value when nothing was probed", got)
	}
}

func TestExpectedLength_OneLengthForEveryReader(t *testing.T) {
	cases := []struct {
		name string
		ac   *service.AcquisitionContext
		want float64
	}{
		{"catalog length wins over a trusted resolved one", &service.AcquisitionContext{
			Track: service.TrackRef{CatalogDuration: 139}, Identity: ports.RecordingIdentity{Duration: 167},
		}, 139},
		{"resolved length when no target length is saved", contextWithLengths(0, 140), 140},
		{"target length when the resolved one is doubted", &service.AcquisitionContext{
			Track: service.TrackRef{CatalogDuration: 139}, Identity: ports.RecordingIdentity{Duration: 167, ReferenceDoubted: true},
		}, 139},
		{"target length when nothing was resolved", contextWithLengths(139, 0), 139},
		{"no length when the only resolved one is doubted", &service.AcquisitionContext{
			Identity: ports.RecordingIdentity{Duration: 167, ReferenceDoubted: true},
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
