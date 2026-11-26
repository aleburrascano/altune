package domain_test

import (
	"altune/go-api/internal/search/internal/domain"
	"testing"
)

func TestBestVersion_RanksADecimalVersionBetweenItsNeighbours(t *testing.T) {
	for _, c := range []struct {
		labels []string
		want   string
	}{
		{[]string{"V3", "V4", "V3.6"}, "V4"},
		{[]string{"V3", "V3.6"}, "V3.6"},
	} {
		versions := make([]domain.UnreleasedVersion, 0, len(c.labels))
		for _, label := range c.labels {
			versions = append(versions, domain.UnreleasedVersion{Label: label})
		}
		song := domain.UnreleasedSong{Versions: versions}
		if got := song.BestVersion().Label; got != c.want {
			t.Errorf("best of %v = %q, want %q", c.labels, got, c.want)
		}
	}
}

func TestBestVersion_ComparesMinorVersionNumbersAsWholeNumbers(t *testing.T) {
	song := domain.UnreleasedSong{Versions: []domain.UnreleasedVersion{{Label: "V3.10"}, {Label: "V3.6"}, {Label: "V3.9"}}}
	if got := song.BestVersion().Label; got != "V3.10" {
		t.Errorf("best of V3.10, V3.6, V3.9 = %q, want V3.10", got)
	}
}
