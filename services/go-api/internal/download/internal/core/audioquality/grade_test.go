package audioquality_test

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"altune/go-api/internal/shared/downloadprovenance"
	"testing"
)

func TestOf(t *testing.T) {
	cases := []struct {
		name        string
		fingerprint audioquality.FingerprintResult
		provenance  downloadprovenance.AcquisitionProvenance
		kind        audioquality.SourceKind
		want        audioquality.Grade
	}{
		{"fingerprint matches an art track", audioquality.Matches, downloadprovenance.ProvenanceVerified, audioquality.ArtTrack, audioquality.A},
		{"fingerprint matches an official video", audioquality.Matches, downloadprovenance.ProvenanceVerified, audioquality.OfficialVideo, audioquality.A},
		{"fingerprint matches a label upload", audioquality.Matches, downloadprovenance.ProvenanceVerified, audioquality.LabelUpload, audioquality.A},
		{"fingerprint matches a fan upload", audioquality.Matches, downloadprovenance.ProvenanceVerified, audioquality.FanUpload, audioquality.B},
		{"fingerprint matches the user's own upload", audioquality.Matches, downloadprovenance.ProvenanceVerified, audioquality.UserUpload, audioquality.B},
		{"fingerprint approximates an art track", audioquality.Approximates, downloadprovenance.ProvenanceVerified, audioquality.ArtTrack, audioquality.B},
		{"length only", audioquality.Silent, downloadprovenance.ProvenanceCorroborated, audioquality.ArtTrack, audioquality.C},
		{"best effort", audioquality.Silent, downloadprovenance.ProvenanceBestEffort, audioquality.ArtTrack, audioquality.D},
		{"no evidence", audioquality.Silent, downloadprovenance.ProvenanceBestEffort, "", audioquality.D},
		{"fingerprint finds a different song but proves nothing", audioquality.Unsupported, downloadprovenance.ProvenanceCorroborated, audioquality.ArtTrack, audioquality.D},
		{"fingerprint proves another version or song", audioquality.Contradicts, downloadprovenance.ProvenanceVerified, audioquality.ArtTrack, audioquality.F},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := audioquality.Of(tc.fingerprint, tc.provenance, tc.kind); got != tc.want {
				t.Errorf("Of = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOf_GradesSortBestToWorstSoAQueryCanOrderWorstFirst(t *testing.T) {
	if audioquality.A >= audioquality.B || audioquality.B >= audioquality.C || audioquality.C >= audioquality.D || audioquality.D >= audioquality.F {
		t.Fatal("grades must sort best to worst as text")
	}
}

func TestBestAvailable_IsNotOnTheLadderTheGradesAreOrderedBy(t *testing.T) {
	for _, g := range []audioquality.Grade{audioquality.A, audioquality.B, audioquality.C, audioquality.D, audioquality.F} {
		if g == audioquality.BestAvailable {
			t.Errorf("%q must differ from every ladder grade", g)
		}
	}
	if audioquality.BestAvailable <= audioquality.F {
		t.Error("best_available must not sort among the grades A..F")
	}
}
