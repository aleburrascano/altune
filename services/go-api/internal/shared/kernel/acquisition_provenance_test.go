package kernel_test

import (
	"altune/go-api/internal/shared/kernel"
	"testing"
)

func TestAcquisitionProvenanceValid(t *testing.T) {
	cases := []struct {
		value kernel.AcquisitionProvenance
		want  bool
	}{
		{kernel.ProvenanceVerified, true},
		{kernel.ProvenanceCorroborated, true},
		{kernel.ProvenanceBestEffort, true},
		{kernel.AcquisitionProvenance("unknown"), false},
	}
	for _, c := range cases {
		if got := c.value.Valid(); got != c.want {
			t.Errorf("%q.Valid() = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestAcquisitionProvenanceWireValues(t *testing.T) {
	if kernel.ProvenanceVerified != "verified" || kernel.ProvenanceCorroborated != "corroborated" || kernel.ProvenanceBestEffort != "best_effort" {
		t.Fatal("provenance wire values changed")
	}
}
