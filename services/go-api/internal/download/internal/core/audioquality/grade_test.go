package audioquality_test

import (
	"altune/go-api/internal/download"
	"altune/go-api/internal/shared/downloadprovenance"
	"testing"
)

func TestOf(t *testing.T) {
	cases := []struct {
		name        string
		fingerprint download.FingerprintResult
		provenance  downloadprovenance.AcquisitionProvenance
		kind        download.SourceKind
		want        download.Grade
	}{
		{"fingerprint matches an art track", download.Matches, downloadprovenance.ProvenanceVerified, download.ArtTrack, download.GradeA},
		{"fingerprint matches an official video", download.Matches, downloadprovenance.ProvenanceVerified, download.OfficialVideo, download.GradeA},
		{"fingerprint matches a label upload", download.Matches, downloadprovenance.ProvenanceVerified, download.LabelUpload, download.GradeA},
		{"fingerprint matches a fan upload", download.Matches, downloadprovenance.ProvenanceVerified, download.FanUpload, download.GradeB},
		{"fingerprint matches the user's own upload", download.Matches, downloadprovenance.ProvenanceVerified, download.UserUpload, download.GradeB},
		{"fingerprint approximates an art track", download.Approximates, downloadprovenance.ProvenanceVerified, download.ArtTrack, download.GradeB},
		{"length only", download.FingerprintSilent, downloadprovenance.ProvenanceCorroborated, download.ArtTrack, download.GradeC},
		{"best effort", download.FingerprintSilent, downloadprovenance.ProvenanceBestEffort, download.ArtTrack, download.GradeD},
		{"no evidence", download.FingerprintSilent, downloadprovenance.ProvenanceBestEffort, "", download.GradeD},
		{"fingerprint finds a different song but proves nothing", download.FingerprintUnsupported, downloadprovenance.ProvenanceCorroborated, download.ArtTrack, download.GradeD},
		{"fingerprint proves another version or song", download.Contradicts, downloadprovenance.ProvenanceVerified, download.ArtTrack, download.GradeF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := download.GradeOf(tc.fingerprint, tc.provenance, tc.kind); got != tc.want {
				t.Errorf("Of = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOf_GradesSortBestToWorstSoAQueryCanOrderWorstFirst(t *testing.T) {
	if download.GradeA >= download.GradeB || download.GradeB >= download.GradeC || download.GradeC >= download.GradeD || download.GradeD >= download.GradeF {
		t.Fatal("grades must sort best to worst as text")
	}
}

func TestBestAvailable_IsNotOnTheLadderTheGradesAreOrderedBy(t *testing.T) {
	for _, g := range []download.Grade{download.GradeA, download.GradeB, download.GradeC, download.GradeD, download.GradeF} {
		if g == download.GradeBestAvailable {
			t.Errorf("%q must differ from every ladder grade", g)
		}
	}
	if download.GradeBestAvailable <= download.GradeF {
		t.Error("best_available must not sort among the grades A..F")
	}
}
