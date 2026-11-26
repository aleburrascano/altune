package querymatch_test

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/core/querymatch"
	"altune/go-api/internal/shared/textnorm"
	"math"
	"testing"
)

func track(title, subtitle string) domain.SearchResult {
	return domain.SearchResult{Kind: domain.ResultKindTrack, Title: title, Subtitle: subtitle}
}

func scoreOf(query string, candidates []domain.SearchResult, target domain.SearchResult) float64 {
	return querymatch.NewScorer(textnorm.NormalizeForMatch(query), candidates).Score(target)
}

func assertScore(t *testing.T, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 1e-9 {
		t.Errorf("score = %.6f, want %.6f", got, want)
	}
}

func TestScore_RecoversMessyMetadataTitleMatch(t *testing.T) {
	set := []domain.SearchResult{
		track("Olympics - Ken Carson, Lil Tecca", "somereuploader"),
		track("Overseas", "Ken Carson"),
		track("Hardcore", "Ken Carson"),
	}
	canonical := scoreOf("Ken Carson Olympics", set, set[0])
	wrongSong := scoreOf("Ken Carson Olympics", set, set[1])

	if canonical <= wrongSong {
		t.Errorf("messy-title canonical (%.3f) must outrank the wrong-song same-artist result (%.3f) — the rare 'olympics' token carries it", canonical, wrongSong)
	}
}

func TestScore_DoesNotOverpromoteArtistInTitleJunk(t *testing.T) {
	set := []domain.SearchResult{
		track("The Way You Make Me Feel (2012 Remaster)", "Michael Jackson"),
		track("Michael Jackson - The Way You Make Me Feel", "djbootleguploader"),
		track("Thriller", "Michael Jackson"),
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

func TestScore_ExactMatchScoresHigh(t *testing.T) {
	set := []domain.SearchResult{
		track("Blinding Lights", "The Weeknd"),
		track("Save Your Tears", "The Weeknd"),
		track("Levitating", "Dua Lipa"),
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

func TestScore_WordsAreWeightedByHowRareTheyAreAmongCandidates(t *testing.T) {
	set := []domain.SearchResult{track("aa bb", ""), track("aa", ""), track("cc", "")}

	assertScore(t, scoreOf("aa bb", set, set[1]), 1.0/3)
	assertScore(t, scoreOf("aa bb", set, set[0]), 1)
}

func TestScore_WithoutCandidatesEveryQueryWordWeighsTheSame(t *testing.T) {
	assertScore(t, scoreOf("aa bb", nil, track("aa", "")), 0.5)
}

func TestScore_FuzzyWordToleranceIsEditDistanceOverTheLongerWord(t *testing.T) {
	assertScore(t, scoreOf("lights", nil, track("Lights", "")), 1)
	assertScore(t, scoreOf("lihgts", nil, track("Lights", "")), 2.0/3)
	if s := scoreOf("olympics", nil, track("Overseas", "")); s >= 0.6 {
		t.Errorf("unrelated words scored %.3f, want low", s)
	}
}

func TestScore_WordsEveryCandidateSharesFallBackToWholeNameSimilarity(t *testing.T) {
	humble := track("Humble", "Kendrick Lamar")
	set := []domain.SearchResult{humble}

	assertScore(t, scoreOf("humble", set, humble), 1)
	assertScore(t, scoreOf("kendrick lamar humble", set, humble), 1)
}

func TestScore_NothingToCompareScoresZero(t *testing.T) {
	assertScore(t, scoreOf("", nil, track("Humble", "Kendrick Lamar")), 0)
	assertScore(t, scoreOf("humble", nil, track("!!!", "")), 0)
}

func TestSharesQueryWord(t *testing.T) {
	humble := track("Humble", "Kendrick Lamar")
	cases := []struct {
		query string
		want  bool
	}{
		{"", true},
		{"a", true},
		{"humble", true},
		{"kendrick dna", true},
		{"ok", false},
		{"dna", false},
	}
	for _, c := range cases {
		if got := querymatch.SharesQueryWord(humble, c.query); got != c.want {
			t.Errorf("SharesQueryWord(Humble, %q) = %v, want %v", c.query, got, c.want)
		}
	}
}
