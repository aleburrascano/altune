package downloadprovenance_test

import (
	"altune/go-api/internal/shared/downloadprovenance"
	"testing"
)

func TestAcquisitionProvenanceValid(t *testing.T) {
	cases := []struct {
		value downloadprovenance.AcquisitionProvenance
		want  bool
	}{
		{downloadprovenance.ProvenanceVerified, true},
		{downloadprovenance.ProvenanceCorroborated, true},
		{downloadprovenance.ProvenanceBestEffort, true},
		{downloadprovenance.AcquisitionProvenance("unknown"), false},
	}
	for _, c := range cases {
		if got := c.value.Valid(); got != c.want {
			t.Errorf("%q.Valid() = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestAcquisitionProvenanceWireValues(t *testing.T) {
	if downloadprovenance.ProvenanceVerified != "verified" || downloadprovenance.ProvenanceCorroborated != "corroborated" || downloadprovenance.ProvenanceBestEffort != "best_effort" {
		t.Fatal("provenance wire values changed")
	}
}
