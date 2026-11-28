package domain_test

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/downloadprovenance"
	"slices"
	"testing"
)

func TestOf(t *testing.T) {
	cases := []struct {
		name        string
		fingerprint domain.FingerprintResult
		provenance  downloadprovenance.AcquisitionProvenance
		kind        domain.SourceKind
		want        domain.Grade
	}{
		{"fingerprint matches an art track", domain.Matches, downloadprovenance.ProvenanceVerified, domain.ArtTrack, domain.A},
		{"fingerprint matches an official video", domain.Matches, downloadprovenance.ProvenanceVerified, domain.OfficialVideo, domain.A},
		{"fingerprint matches a label upload", domain.Matches, downloadprovenance.ProvenanceVerified, domain.LabelUpload, domain.A},
		{"fingerprint matches a fan upload", domain.Matches, downloadprovenance.ProvenanceVerified, domain.FanUpload, domain.B},
		{"fingerprint matches the user's own upload", domain.Matches, downloadprovenance.ProvenanceVerified, domain.UserUpload, domain.B},
		{"fingerprint approximates an art track", domain.Approximates, downloadprovenance.ProvenanceVerified, domain.ArtTrack, domain.B},
		{"length only", domain.Silent, downloadprovenance.ProvenanceCorroborated, domain.ArtTrack, domain.C},
		{"best effort", domain.Silent, downloadprovenance.ProvenanceBestEffort, domain.ArtTrack, domain.D},
		{"no evidence", domain.Silent, downloadprovenance.ProvenanceBestEffort, "", domain.D},
		{"fingerprint finds a different song but proves nothing", domain.Unsupported, downloadprovenance.ProvenanceCorroborated, domain.ArtTrack, domain.D},
		{"fingerprint proves another version or song", domain.Contradicts, downloadprovenance.ProvenanceVerified, domain.ArtTrack, domain.F},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.Of(tc.fingerprint, tc.provenance, tc.kind); got != tc.want {
				t.Errorf("Of = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOf_GradesSortBestToWorstSoAQueryCanOrderWorstFirst(t *testing.T) {
	if domain.A >= domain.B || domain.B >= domain.C || domain.C >= domain.D || domain.D >= domain.F {
		t.Fatal("grades must sort best to worst as text")
	}
}

func TestBestAvailable_IsNotOnTheLadderTheGradesAreOrderedBy(t *testing.T) {
	for _, g := range []domain.Grade{domain.A, domain.B, domain.C, domain.D, domain.F} {
		if g == domain.BestAvailable {
			t.Errorf("%q must differ from every ladder grade", g)
		}
	}
	if domain.BestAvailable <= domain.F {
		t.Error("best_available must not sort among the grades A..F")
	}
}

func TestWorstFirst_ListsEveryRankedGradeWorstToBestAndIsACopy(t *testing.T) {
	want := []domain.Grade{domain.F, domain.D, domain.C, domain.B, domain.A, domain.AUser}
	got := domain.WorstFirst()
	if !slices.Equal(got, want) {
		t.Fatalf("WorstFirst() = %v, want %v", got, want)
	}
	got[0] = domain.A
	if again := domain.WorstFirst(); again[0] != domain.F {
		t.Errorf("WorstFirst()[0] = %q after a caller changed its copy, want %q", again[0], domain.F)
	}
}
