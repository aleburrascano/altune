package grade

import (
	"altune/go-api/internal/shared"
	"testing"
)

func TestOf(t *testing.T) {
	cases := []struct {
		name        string
		fingerprint Fingerprint
		provenance  shared.AcquisitionProvenance
		kind        SourceKind
		want        Grade
	}{
		{"fingerprint matches an art track", Matches, shared.ProvenanceVerified, ArtTrack, A},
		{"fingerprint matches an official video", Matches, shared.ProvenanceVerified, OfficialVideo, A},
		{"fingerprint matches a label upload", Matches, shared.ProvenanceVerified, LabelUpload, A},
		{"fingerprint matches a fan upload", Matches, shared.ProvenanceVerified, FanUpload, B},
		{"fingerprint matches the user's own upload", Matches, shared.ProvenanceVerified, UserUpload, B},
		{"fingerprint approximates an art track", Approximates, shared.ProvenanceVerified, ArtTrack, B},
		{"length only", Silent, shared.ProvenanceCorroborated, ArtTrack, C},
		{"best effort", Silent, shared.ProvenanceBestEffort, ArtTrack, D},
		{"no evidence", Silent, shared.ProvenanceBestEffort, "", D},
		{"fingerprint finds a different song but proves nothing", Unsupported, shared.ProvenanceCorroborated, ArtTrack, D},
		{"fingerprint proves another version or song", Contradicts, shared.ProvenanceVerified, ArtTrack, F},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Of(tc.fingerprint, tc.provenance, tc.kind); got != tc.want {
				t.Errorf("Of = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOf_GradesSortBestToWorstSoAQueryCanOrderWorstFirst(t *testing.T) {
	if A >= B || B >= C || C >= D || D >= F {
		t.Fatal("grades must sort best to worst as text")
	}
}

func TestBestAvailable_IsNotOnTheLadderTheGradesAreOrderedBy(t *testing.T) {
	for _, g := range []Grade{A, B, C, D, F} {
		if g == BestAvailable {
			t.Errorf("%q must differ from every ladder grade", g)
		}
	}
	if BestAvailable <= F {
		t.Error("best_available must not sort among the grades A..F")
	}
}
