package service

import (
	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
	"testing"
)

func contextWithLengths(saved, resolved float64) *AcquisitionContext {
	return &AcquisitionContext{
		Track:    TrackRef{Duration: saved},
		Identity: ports.RecordingIdentity{Duration: resolved},
	}
}

func TestDurationAcceptable_DisagreementFallsBackToTheLooseWindow(t *testing.T) {
	ac := contextWithLengths(232, 276)

	if !ac.durationAcceptable(276) {
		t.Error("the resolved album cut must be acceptable when the two lengths disagree")
	}
	if !ac.durationAcceptable(232) {
		t.Error("the saved single edit must stay acceptable too — neither claim is proven")
	}
	if ac.durationAcceptable(600) {
		t.Error("a 10-minute upload is outside both windows and must still be rejected")
	}
}

func TestDurationAcceptable_AgreementKeepsTheTightWindow(t *testing.T) {
	ac := contextWithLengths(262, 263)

	if !ac.durationAcceptable(264) {
		t.Error("ordinary encoding trim must pass a corroborated length")
	}
	if ac.durationAcceptable(276) {
		t.Error("a 14s excess against a corroborated length is contamination and must be rejected")
	}
}

func TestDurationAcceptable_NoResolvedLengthKeepsTheLooseWindow(t *testing.T) {
	ac := contextWithLengths(200, 0)

	if !ac.durationAcceptable(210) {
		t.Error("without corroboration the 15s window applies")
	}
	if ac.durationAcceptable(260) {
		t.Error("60s off is a different recording under any window")
	}
}

func TestLengthCorroborated_RequiresAgreement(t *testing.T) {
	if contextWithLengths(232, 276).lengthCorroborated() {
		t.Error("two conflicting claims are not corroboration")
	}
	if !contextWithLengths(262, 263).lengthCorroborated() {
		t.Error("two independent sources agreeing is exactly what corroboration means")
	}
	if !contextWithLengths(0, 262).lengthCorroborated() {
		t.Error("a resolved length with nothing to contradict it is corroborated")
	}
}

func TestProvenance_DisagreementIsNeverCorroborated(t *testing.T) {
	ac := contextWithLengths(232, 276)
	ac.DurationVerified = true

	if got := ac.Provenance(); got != domain.ProvenanceBestEffort {
		t.Errorf("Provenance() = %q, want best_effort — a length that passed a window it disagrees with proves nothing", got)
	}
}

func TestMeasuredDuration_PrefersTheProbedValue(t *testing.T) {
	ac := &AcquisitionContext{
		ProbedDuration: 232,
		Selected:       &ports.AudioCandidate{Duration: 999},
	}
	if got := ac.MeasuredDuration(); got != 232 {
		t.Errorf("MeasuredDuration() = %v, want the probed 232 rather than provider metadata", got)
	}

	ac = &AcquisitionContext{Selected: &ports.AudioCandidate{Duration: 240}}
	if got := ac.MeasuredDuration(); got != 240 {
		t.Errorf("MeasuredDuration() = %v, want the provider value when nothing was probed", got)
	}
}

func TestDurationAcceptable_TightensWhenCorroborated(t *testing.T) {
	loose := AcquisitionContext{Track: TrackRef{Duration: 181}}
	if !loose.durationAcceptable(193) {
		t.Error("without a corroborated length, a 12s excess is inside the loose tolerance")
	}

	tight := AcquisitionContext{
		Track:    TrackRef{Duration: 181},
		Identity: ports.RecordingIdentity{Duration: 181},
	}
	if tight.durationAcceptable(193) {
		t.Error("with a corroborated length, a 12s excess is contamination and must be rejected")
	}
	if !tight.durationAcceptable(184) {
		t.Error("a 3s difference is ordinary encoding trim and must still pass")
	}
}

func TestExpectedLength_OneLengthForEveryReader(t *testing.T) {
	cases := []struct {
		name string
		ac   *AcquisitionContext
		want float64
	}{
		{"catalog length wins over a trusted resolved one", &AcquisitionContext{
			Track: TrackRef{Duration: 139, CatalogDuration: 139}, Identity: ports.RecordingIdentity{Duration: 167},
		}, 139},
		{"resolved length when not doubted", contextWithLengths(139, 140), 140},
		{"saved length when the resolved one is doubted", &AcquisitionContext{
			Track: TrackRef{Duration: 139}, Identity: ports.RecordingIdentity{Duration: 167, ReferenceDoubted: true},
		}, 139},
		{"saved length when nothing was resolved", contextWithLengths(139, 0), 139},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.ac.ExpectedLength(); got != tc.want {
				t.Errorf("ExpectedLength = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestDurationDelta_ReadsTheExpectedLengthNotTheDoubtedSearchPick(t *testing.T) {
	ac := &AcquisitionContext{
		Track:    TrackRef{Duration: 139, CatalogDuration: 139},
		Identity: ports.RecordingIdentity{Duration: 167, ReferenceDoubted: true},
	}

	if got := ac.durationDelta(ports.AudioCandidate{Duration: 140}, 140); got != 1 {
		t.Errorf("durationDelta = %v, want 1 against the catalog 139s", got)
	}
}

func TestDurationAcceptable_ADoubtedResolvedLengthWidensNothing(t *testing.T) {
	ac := &AcquisitionContext{
		Track:    TrackRef{Duration: 139},
		Identity: ports.RecordingIdentity{Duration: 167, ReferenceDoubted: true},
	}

	if ac.durationAcceptable(167) {
		t.Error("a 167s file matches only the doubted search pick and must be rejected")
	}
}

func TestReferenceFor_CarriesTheExpectedLength(t *testing.T) {
	ac := &AcquisitionContext{
		Track:    TrackRef{Duration: 139, CatalogDuration: 139},
		Identity: ports.RecordingIdentity{Duration: 167, ReferenceDoubted: true},
	}

	if got := referenceFor(ac).Duration; got != 139 {
		t.Errorf("reference duration = %v, want 139", got)
	}
}
