package domain

import (
	"math"
	"testing"
)

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
	// Jaccard is weighted above phonetic, so a candidate matching only on trigram
	// overlap must outrank one matching only phonetically.
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
