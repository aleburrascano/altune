package service

import (
	"math"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

func trackEntity(title, subtitle string) Entity {
	return Entity{Result: domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    title,
		Subtitle: subtitle,
	}}
}

func scoreOf(q string, set []Entity, target Entity) float64 {
	qn := textnorm.NormalizeForMatch(q)
	rarity := queryTokenRarity(qn, set)
	return idfWeightedCoverage(target.Result, qn, rarity)
}

func TestIDFCoverage_RecoversMessyMetadataTitleMatch(t *testing.T) {
	set := []Entity{
		trackEntity("Olympics - Ken Carson, Lil Tecca", "somereuploader"),
		trackEntity("Overseas", "Ken Carson"),
		trackEntity("Hardcore", "Ken Carson"),
	}
	canonical := scoreOf("Ken Carson Olympics", set, set[0])
	wrongSong := scoreOf("Ken Carson Olympics", set, set[1])

	if canonical <= wrongSong {
		t.Errorf("messy-title canonical (%.3f) must outrank the wrong-song same-artist result (%.3f) — the rare 'olympics' token carries it", canonical, wrongSong)
	}
}

func TestIDFCoverage_DoesNotOverpromoteArtistInTitleJunk(t *testing.T) {
	set := []Entity{
		trackEntity("The Way You Make Me Feel (2012 Remaster)", "Michael Jackson"),
		trackEntity("Michael Jackson - The Way You Make Me Feel", "djbootleguploader"),
		trackEntity("Thriller", "Michael Jackson"),
	}
	q := "Michael Jackson The Way You Make Me Feel"
	canonical := scoreOf(q, set, set[0])
	junk := scoreOf(q, set, set[1])

	if junk > canonical+1e-9 {
		t.Errorf("artist-in-title junk (%.3f) must NOT outrank the canonical (%.3f) — that was the boost's bug", junk, canonical)
	}
	if math.Abs(canonical-junk) > 0.05 {
		t.Errorf("canonical (%.3f) and junk (%.3f) should tie on relevance over title+subtitle so the count ladder decides", canonical, junk)
	}
}

func TestIDFCoverage_ExactMatchScoresHigh(t *testing.T) {
	set := []Entity{
		trackEntity("Blinding Lights", "The Weeknd"),
		trackEntity("Save Your Tears", "The Weeknd"),
		trackEntity("Levitating", "Dua Lipa"),
	}
	exact := scoreOf("The Weeknd Blinding Lights", set, set[0])
	other := scoreOf("The Weeknd Blinding Lights", set, set[1])
	if exact <= other {
		t.Errorf("exact match (%.3f) must outrank a same-artist different song (%.3f)", exact, other)
	}
	if exact < 0.5 {
		t.Errorf("exact match scored unexpectedly low: %.3f", exact)
	}
}

func TestTokenSimilarity_FuzzyTolerance(t *testing.T) {
	if s := tokenSimilarity("lights", "lights"); s != 1 {
		t.Errorf("exact token similarity = %.3f, want 1", s)
	}
	if s := tokenSimilarity("lights", "lihgts"); s <= 0.5 || s >= 1 {
		t.Errorf("one-transposition typo similarity = %.3f, want a high-but-<1 partial", s)
	}
	if s := tokenSimilarity("olympics", "overseas"); s >= 0.6 {
		t.Errorf("unrelated tokens similarity = %.3f, want low", s)
	}
}
