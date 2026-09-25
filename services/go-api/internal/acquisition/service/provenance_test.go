package service

import (
	"altune/go-api/internal/catalog/domain"
	"testing"
)

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
