package grade_test

import (
	"altune/go-api/internal/acquisition"
	shared "altune/go-api/internal/shared/kernel"
	"testing"
)

func TestOf(t *testing.T) {
	cases := []struct {
		name        string
		fingerprint acquisition.GradeFingerprint
		provenance  shared.AcquisitionProvenance
		kind        acquisition.SourceKind
		want        acquisition.Grade
	}{
		{"fingerprint matches an art track", acquisition.Matches, shared.ProvenanceVerified, acquisition.ArtTrack, acquisition.GradeA},
		{"fingerprint matches an official video", acquisition.Matches, shared.ProvenanceVerified, acquisition.OfficialVideo, acquisition.GradeA},
		{"fingerprint matches a label upload", acquisition.Matches, shared.ProvenanceVerified, acquisition.LabelUpload, acquisition.GradeA},
		{"fingerprint matches a fan upload", acquisition.Matches, shared.ProvenanceVerified, acquisition.FanUpload, acquisition.GradeB},
		{"fingerprint matches the user's own upload", acquisition.Matches, shared.ProvenanceVerified, acquisition.UserUpload, acquisition.GradeB},
		{"fingerprint approximates an art track", acquisition.Approximates, shared.ProvenanceVerified, acquisition.ArtTrack, acquisition.GradeB},
		{"length only", acquisition.FingerprintSilent, shared.ProvenanceCorroborated, acquisition.ArtTrack, acquisition.GradeC},
		{"best effort", acquisition.FingerprintSilent, shared.ProvenanceBestEffort, acquisition.ArtTrack, acquisition.GradeD},
		{"no evidence", acquisition.FingerprintSilent, shared.ProvenanceBestEffort, "", acquisition.GradeD},
		{"fingerprint finds a different song but proves nothing", acquisition.FingerprintUnsupported, shared.ProvenanceCorroborated, acquisition.ArtTrack, acquisition.GradeD},
		{"fingerprint proves another version or song", acquisition.Contradicts, shared.ProvenanceVerified, acquisition.ArtTrack, acquisition.GradeF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := acquisition.GradeOf(tc.fingerprint, tc.provenance, tc.kind); got != tc.want {
				t.Errorf("Of = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOf_GradesSortBestToWorstSoAQueryCanOrderWorstFirst(t *testing.T) {
	if acquisition.GradeA >= acquisition.GradeB || acquisition.GradeB >= acquisition.GradeC || acquisition.GradeC >= acquisition.GradeD || acquisition.GradeD >= acquisition.GradeF {
		t.Fatal("grades must sort best to worst as text")
	}
}

func TestBestAvailable_IsNotOnTheLadderTheGradesAreOrderedBy(t *testing.T) {
	for _, g := range []acquisition.Grade{acquisition.GradeA, acquisition.GradeB, acquisition.GradeC, acquisition.GradeD, acquisition.GradeF} {
		if g == acquisition.GradeBestAvailable {
			t.Errorf("%q must differ from every ladder grade", g)
		}
	}
	if acquisition.GradeBestAvailable <= acquisition.GradeF {
		t.Error("best_available must not sort among the grades A..F")
	}
}
