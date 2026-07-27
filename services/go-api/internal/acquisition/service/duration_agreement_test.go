package service

import (
	"testing"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
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
