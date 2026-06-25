package service

import (
	"math"
	"testing"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"
)

// track builds a track Entity with the given title and subtitle (artist).
func trackEntity(title, subtitle string) Entity {
	return Entity{Result: domain.SearchResult{
		Kind:     domain.ResultKindTrack,
		Title:    title,
		Subtitle: subtitle,
	}}
}

// scoreOf computes the parameter-free relevance of one entity against the query,
// with IDF weights derived from the whole candidate set.
func scoreOf(q string, set []Entity, target Entity) float64 {
	qn := textnorm.NormalizeForMatch(q)
	rarity := queryTokenRarity(qn, set)
	return idfWeightedCoverage(target.Result, qn, rarity)
}

// TestIDFCoverage_RecoversMessyMetadataTitleMatch is the boost's win, kept without
// the boost: a query whose rare token names the song must rank the result that
// carries that token in its (messy) title above a clean same-artist result that
// misses the song.
func TestIDFCoverage_RecoversMessyMetadataTitleMatch(t *testing.T) {
	set := []Entity{
		trackEntity("Olympics - Ken Carson, Lil Tecca", "somereuploader"), // canonical, uploader in artist field
		trackEntity("Overseas", "Ken Carson"),                             // right artist, wrong song
		trackEntity("Hardcore", "Ken Carson"),                             // another wrong song, same artist
	}
	canonical := scoreOf("Ken Carson Olympics", set, set[0])
	wrongSong := scoreOf("Ken Carson Olympics", set, set[1])

	if canonical <= wrongSong {
		t.Errorf("messy-title canonical (%.3f) must outrank the wrong-song same-artist result (%.3f) — the rare 'olympics' token carries it", canonical, wrongSong)
	}
}

// TestIDFCoverage_DoesNotOverpromoteArtistInTitleJunk is the boost's FAILURE mode,
// fixed: for an "artist + common-title" query, a single-source junk upload that
// stuffs the artist into its title must NOT score higher than the canonical that
// carries the artist in its subtitle. Computed over title+subtitle, they tie — and
// the multi-source count ladder (not relevance) then picks the canonical.
func TestIDFCoverage_DoesNotOverpromoteArtistInTitleJunk(t *testing.T) {
	set := []Entity{
		trackEntity("The Way You Make Me Feel (2012 Remaster)", "Michael Jackson"),  // canonical: artist in subtitle
		trackEntity("Michael Jackson - The Way You Make Me Feel", "djbootleguploader"), // junk: artist in title
		trackEntity("Thriller", "Michael Jackson"),                                  // distractor so MJ tokens aren't all ubiquitous
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

// TestIDFCoverage_ExactMatchScoresHigh sanity-checks that an exact "artist title"
// match scores near the top of its candidate set.
func TestIDFCoverage_ExactMatchScoresHigh(t *testing.T) {
	set := []Entity{
		trackEntity("Blinding Lights", "The Weeknd"),
		trackEntity("Save Your Tears", "The Weeknd"),
		trackEntity("Levitating", "Dua Lipa"),
	}
	exact := scoreOf("The Weeknd Blinding Lights", set, set[0])
	other := scoreOf("The Weeknd Blinding Lights", set, set[1]) // same artist, different song
	if exact <= other {
		t.Errorf("exact match (%.3f) must outrank a same-artist different song (%.3f)", exact, other)
	}
	if exact < 0.5 {
		t.Errorf("exact match scored unexpectedly low: %.3f", exact)
	}
}

// TestTokenSimilarity_FuzzyTolerance confirms the per-token match is continuous
// (typo tolerance survives) and exact-equal scores 1.
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
