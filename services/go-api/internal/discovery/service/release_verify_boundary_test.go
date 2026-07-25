package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestFilterGroupsByMBAnchor_anchorCredibilityBoundary(t *testing.T) {
	misfit := verifyGroup(domain.ProviderDeezer, "w", "x", "y", "z")

	weak := normalizeTitleSet(numberedTitles("rap", 4))
	if got := FilterGroupsByMBAnchor(weak, []ReleaseGroup{misfit}); len(got) != 1 {
		t.Errorf("4 MB titles: kept %d, want 1 (anchor below minimum verifies nothing)", len(got))
	}

	credible := normalizeTitleSet(numberedTitles("rap", 5))
	if got := FilterGroupsByMBAnchor(credible, []ReleaseGroup{misfit}); len(got) != 0 {
		t.Errorf("5 MB titles: kept %d, want 0 (anchor exactly at minimum verifies)", len(got))
	}
}

func TestGroupMatchesAnchor_providerSizeBoundary(t *testing.T) {
	mb := normalizeTitleSet(numberedTitles("rap", 10))
	three := verifyGroup(domain.ProviderDeezer, "a", "b", "c")
	if got := FilterGroupsByMBAnchor(mb, []ReleaseGroup{three}); len(got) != 1 {
		t.Errorf("3 titles: kept %d, want 1 (too few to judge)", len(got))
	}
	four := verifyGroup(domain.ProviderDeezer, "a", "b", "c", "d")
	if got := FilterGroupsByMBAnchor(mb, []ReleaseGroup{four}); len(got) != 0 {
		t.Errorf("4 titles: kept %d, want 0 (exactly at the judging minimum)", len(got))
	}
}

func TestGroupMatchesAnchor_overlapBoundary(t *testing.T) {
	mb := normalizeTitleSet(numberedTitles("rap", 40))
	titles := append(numberedTitles("own", 16), "rap 0", "rap 1", "rap 2", "rap 3")
	overlap4 := verifyGroup(domain.ProviderDeezer, titles...)
	if got := FilterGroupsByMBAnchor(mb, []ReleaseGroup{overlap4}); len(got) != 1 {
		t.Errorf("overlap exactly 4: kept %d, want 1", len(got))
	}
	titles3 := append(numberedTitles("own", 17), "rap 0", "rap 1", "rap 2")
	overlap3 := verifyGroup(domain.ProviderDeezer, titles3...)
	if got := FilterGroupsByMBAnchor(mb, []ReleaseGroup{overlap3}); len(got) != 0 {
		t.Errorf("overlap 3 / ratio 0.15: kept %d, want 0", len(got))
	}
}

func TestGroupMatchesAnchor_ratioExactlyAtQuarter(t *testing.T) {
	mb := normalizeTitleSet(numberedTitles("rap", 10))
	titles := append(numberedTitles("own", 6), "rap 0", "rap 1")
	quarter := verifyGroup(domain.ProviderDeezer, titles...)
	if got := FilterGroupsByMBAnchor(mb, []ReleaseGroup{quarter}); len(got) != 1 {
		t.Errorf("ratio exactly 0.25: kept %d, want 1 (inclusive boundary)", len(got))
	}
	titles9 := append(numberedTitles("own", 7), "rap 0", "rap 1")
	below := verifyGroup(domain.ProviderDeezer, titles9...)
	if got := FilterGroupsByMBAnchor(mb, []ReleaseGroup{below}); len(got) != 0 {
		t.Errorf("ratio just below 0.25: kept %d, want 0", len(got))
	}
}

func TestGroupMatchesAnchor_duplicateTitlesCountOnce(t *testing.T) {
	mb := normalizeTitleSet(numberedTitles("rap", 10))
	dup := verifyGroup(domain.ProviderDeezer, "a", "a", "a", "a", "b", "c")
	if got := FilterGroupsByMBAnchor(mb, []ReleaseGroup{dup}); len(got) != 1 {
		t.Errorf("duplicated titles: kept %d, want 1 (3 distinct titles — too few to judge)", len(got))
	}
}
