package music_test

import (
	"altune/go-api/internal/shared/music"
	"testing"
)

func TestAcquisitionProvenanceValid(t *testing.T) {
	cases := []struct {
		value music.AcquisitionProvenance
		want  bool
	}{
		{music.ProvenanceVerified, true},
		{music.ProvenanceCorroborated, true},
		{music.ProvenanceBestEffort, true},
		{music.AcquisitionProvenance("unknown"), false},
	}
	for _, c := range cases {
		if got := c.value.Valid(); got != c.want {
			t.Errorf("%q.Valid() = %v, want %v", c.value, got, c.want)
		}
	}
}

func TestAcquisitionProvenanceWireValues(t *testing.T) {
	if music.ProvenanceVerified != "verified" || music.ProvenanceCorroborated != "corroborated" || music.ProvenanceBestEffort != "best_effort" {
		t.Fatal("provenance wire values changed")
	}
}
