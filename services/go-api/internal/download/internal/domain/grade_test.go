package domain_test

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/music"
	"slices"
	"testing"
)

func TestOf(t *testing.T) {
	cases := []struct {
		name        string
		fingerprint domain.FingerprintResult
		provenance  music.AcquisitionProvenance
		kind        domain.SourceKind
		want        domain.Grade
	}{
		{"fingerprint matches an art track", domain.FingerprintMatches, music.ProvenanceVerified, domain.ArtTrack, domain.GradeA},
		{"fingerprint matches an official video", domain.FingerprintMatches, music.ProvenanceVerified, domain.OfficialVideo, domain.GradeA},
		{"fingerprint matches a label upload", domain.FingerprintMatches, music.ProvenanceVerified, domain.LabelUpload, domain.GradeA},
		{"fingerprint matches a fan upload", domain.FingerprintMatches, music.ProvenanceVerified, domain.FanUpload, domain.GradeB},
		{"fingerprint matches the user's own upload", domain.FingerprintMatches, music.ProvenanceVerified, domain.UserUpload, domain.GradeB},
		{"fingerprint approximates an art track", domain.FingerprintApproximates, music.ProvenanceVerified, domain.ArtTrack, domain.GradeB},
		{"length only", domain.FingerprintSilent, music.ProvenanceCorroborated, domain.ArtTrack, domain.GradeC},
		{"best effort", domain.FingerprintSilent, music.ProvenanceBestEffort, domain.ArtTrack, domain.GradeD},
		{"no evidence", domain.FingerprintSilent, music.ProvenanceBestEffort, "", domain.GradeD},
		{"fingerprint finds a different song but proves nothing", domain.FingerprintUnsupported, music.ProvenanceCorroborated, domain.ArtTrack, domain.GradeD},
		{"fingerprint proves another version or song", domain.FingerprintContradicts, music.ProvenanceVerified, domain.ArtTrack, domain.GradeF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := domain.GradeOf(tc.fingerprint, tc.provenance, tc.kind); got != tc.want {
				t.Errorf("Of = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestOf_GradesSortBestToWorstSoAQueryCanOrderWorstFirst(t *testing.T) {
	if domain.GradeA >= domain.GradeB || domain.GradeB >= domain.GradeC || domain.GradeC >= domain.GradeD || domain.GradeD >= domain.GradeF {
		t.Fatal("grades must sort best to worst as text")
	}
}

func TestBestAvailable_IsNotOnTheLadderTheGradesAreOrderedBy(t *testing.T) {
	for _, g := range []domain.Grade{domain.GradeA, domain.GradeB, domain.GradeC, domain.GradeD, domain.GradeF} {
		if g == domain.BestAvailable {
			t.Errorf("%q must differ from every ladder grade", g)
		}
	}
	if domain.BestAvailable <= domain.GradeF {
		t.Error("best_available must not sort among the grades A..F")
	}
}

func TestWorstFirst_ListsEveryRankedGradeWorstToBestAndIsACopy(t *testing.T) {
	want := []domain.Grade{domain.GradeF, domain.GradeD, domain.GradeC, domain.GradeB, domain.GradeA, domain.GradeAUser}
	got := domain.WorstFirst()
	if !slices.Equal(got, want) {
		t.Fatalf("WorstFirst() = %v, want %v", got, want)
	}
	got[0] = domain.GradeA
	if again := domain.WorstFirst(); again[0] != domain.GradeF {
		t.Errorf("WorstFirst()[0] = %q after a caller changed its copy, want %q", again[0], domain.GradeF)
	}
}
