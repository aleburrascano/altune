package service

import (
	"testing"

	"altune/go-api/internal/acquisition/ports"
	"altune/go-api/internal/catalog/domain"
)

func TestProvenance(t *testing.T) {
	tests := []struct {
		name string
		ac   AcquisitionContext
		want domain.AcquisitionProvenance
	}{
		{
			name: "fingerprint match is the strongest claim",
			ac: AcquisitionContext{
				IdentityVerified: true,
				DurationVerified: true,
				Identity:         ports.RecordingIdentity{Duration: 181},
			},
			want: domain.ProvenanceVerified,
		},
		{
			name: "length corroborated by discovery but no fingerprint",
			ac: AcquisitionContext{
				DurationVerified: true,
				Identity:         ports.RecordingIdentity{Duration: 181},
			},
			want: domain.ProvenanceCorroborated,
		},
		{
			name: "length checked against saved metadata only",
			ac:   AcquisitionContext{DurationVerified: true},
			want: domain.ProvenanceBestEffort,
		},
		{
			name: "nothing verified",
			ac:   AcquisitionContext{},
			want: domain.ProvenanceBestEffort,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.ac.Provenance(); got != tt.want {
				t.Errorf("Provenance() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSetAcquisitionProvenance_RejectsUnknownValues(t *testing.T) {
	track := &domain.Track{}

	track.SetAcquisitionProvenance("nonsense")
	if track.AcquisitionProvenance != nil {
		t.Errorf("an unknown provenance must not be written, got %q", *track.AcquisitionProvenance)
	}

	track.SetAcquisitionProvenance(domain.ProvenanceVerified)
	if track.AcquisitionProvenance == nil || *track.AcquisitionProvenance != "verified" {
		t.Errorf("provenance = %v, want verified", track.AcquisitionProvenance)
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
