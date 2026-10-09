package domain

import (
	"math"
	"strings"
	"testing"
)

func TestIsIndexableVocabularyTerm(t *testing.T) {
	atCap := strings.Repeat("é", MaxVocabularyTermRunes)
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
		if got := IsIndexableVocabularyTerm(c.term, c.norm); got != c.want {
			t.Errorf("IsIndexableVocabularyTerm(len %d, len %d) = %v, want %v",
				len([]rune(c.term)), len([]rune(c.norm)), got, c.want)
		}
	}
}

func TestVocabularyMatchScore_PerfectMatchScoresOne(t *testing.T) {
	got := VocabularyMatchScore(1, 1, 1, 1)
	if math.Abs(got-1.0) > 1e-9 {
		t.Fatalf("all-perfect signals should sum to 1.0 (weights), got %v", got)
	}
}

func TestVocabularyMatchScore_NoSignalScoresZero(t *testing.T) {
	if got := VocabularyMatchScore(0, 0, 0, 0); got != 0 {
		t.Fatalf("no signal should score 0, got %v", got)
	}
}

func TestVocabularyMatchScore_WeightsTradeOff(t *testing.T) {
	jaccardOnly := VocabularyMatchScore(1, 0, 0, 0)
	phoneticOnly := VocabularyMatchScore(0, 0, 1, 0)
	if jaccardOnly <= phoneticOnly {
		t.Fatalf("jaccard (0.35) should outweigh phonetic (0.20): %v vs %v", jaccardOnly, phoneticOnly)
	}
}

func TestVocabularyMatchScore_MonotonicInEachSignal(t *testing.T) {
	base := VocabularyMatchScore(0.5, 0.5, 0.5, 0.5)
	if VocabularyMatchScore(0.6, 0.5, 0.5, 0.5) <= base {
		t.Fatalf("raising jaccard must raise the score")
	}
	if VocabularyMatchScore(0.5, 0.5, 0.5, 0.6) <= base {
		t.Fatalf("raising length similarity must raise the score")
	}
}
