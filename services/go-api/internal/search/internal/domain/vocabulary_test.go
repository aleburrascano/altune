package domain_test

import (
	"altune/go-api/internal/search/internal/domain"
	"math"
	"strings"
	"testing"
)

func TestIsIndexableVocabularyTerm(t *testing.T) {
	atCap := strings.Repeat("é", domain.MaxVocabularyTermRunes)
	over := atCap + "x"
	cases := []struct {
		term, norm string
		want       bool
	}{
		{"Kendrick Lamar", "kendrick lamar", true},
		{atCap, atCap, true},
		{over, "short", false},
		{"short", over, false},
	}
	for _, c := range cases {
		if got := domain.IsIndexableVocabularyTerm(c.term, c.norm); got != c.want {
			t.Errorf("IsIndexableVocabularyTerm(len %d, len %d) = %v, want %v",
				len([]rune(c.term)), len([]rune(c.norm)), got, c.want)
		}
	}
}

func TestVocabularyMatchScore_PerfectMatchScoresOne(t *testing.T) {
	got := domain.SpellingHintsMatchScore(1, 1, 1, 1)
	if math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("all-perfect signals should sum to 1.0 (weights), got %v", got)
	}
}

func TestVocabularyMatchScore_NoSignalScoresZero(t *testing.T) {
	if got := domain.SpellingHintsMatchScore(0, 0, 0, 0); got != 0 {
		t.Fatalf("no signal should score 0, got %v", got)
	}
}

func TestVocabularyMatchScore_WeightsTradeOff(t *testing.T) {
	jaccardOnly := domain.SpellingHintsMatchScore(1, 0, 0, 0)
	phoneticOnly := domain.SpellingHintsMatchScore(0, 0, 1, 0)
	if jaccardOnly <= phoneticOnly {
		t.Fatalf("jaccard (0.35) should outweigh phonetic (0.20): %v vs %v", jaccardOnly, phoneticOnly)
	}
}

func TestVocabularyMatchScore_MonotonicInEachSignal(t *testing.T) {
	base := domain.SpellingHintsMatchScore(0.5, 0.5, 0.5, 0.5)
	if domain.SpellingHintsMatchScore(0.6, 0.5, 0.5, 0.5) <= base {
		t.Fatalf("raising jaccard must raise the score")
	}
	if domain.SpellingHintsMatchScore(0.5, 0.5, 0.5, 0.6) <= base {
		t.Fatalf("raising length similarity must raise the score")
	}
}

func TestMaxEditDistance_BandsByRuneCount(t *testing.T) {
	cases := map[string]struct {
		query string
		want  int
	}{
		"empty":            {"", 1},
		"four runes":       {"abcd", 1},
		"five runes":       {"abcde", 2},
		"eight runes":      {"abcdefgh", 2},
		"nine runes":       {"abcdefghi", 3},
		"counts runes":     {"éééé", 1},
		"multibyte medium": {"ééééé", 2},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := domain.MaxEditDistance(tc.query); got != tc.want {
				t.Fatalf("MaxEditDistance(%q) = %d, want %d", tc.query, got, tc.want)
			}
		})
	}
}
