package shared

import "testing"

func TestAcquisitionProvenanceValid(t *testing.T) {
	cases := []struct {
		value AcquisitionProvenance
		want  bool
	}{
		{ProvenanceVerified, true},
		{ProvenanceCorroborated, true},
		{ProvenanceBestEffort, true},
		{AcquisitionProvenance("unknown"), false},
	}
	for _, c := range cases {
		if got := c.value.Valid(); got != c.want {
			t.Errorf("%q.Valid() = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestAcquisitionProvenanceWireValues(t *testing.T) {
	if ProvenanceVerified != "verified" || ProvenanceCorroborated != "corroborated" || ProvenanceBestEffort != "best_effort" {
		t.Fatal("provenance wire values changed")
	}
}
