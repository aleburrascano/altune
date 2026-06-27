package eval

import (
	"math"
	"testing"
)

func TestReplayCorpus_ScoresPositivesAndNegativeLeak(t *testing.T) {
	corpus := BehavioralCorpus{Entries: []BehavioralCorpusEntry{
		{Query: "adele hello", ResultSignature: "good", Polarity: 1},
		{Query: "drake", ResultSignature: "missing", Polarity: 1},
		{Query: "wrong", ResultSignature: "bad", Polarity: -1},
	}}
	ranking := CandidateRanking{
		"adele hello": {"good", "x", "y"}, // positive at rank 0 → rr = 1.0
		"drake":       {"a", "b"},         // positive not ranked → rr = 0
		"wrong":       {"z", "bad"},       // negative leaks into top-3
	}

	score := ReplayCorpus(corpus, ranking, 3)

	if score.Positives != 2 || score.Found != 1 {
		t.Errorf("positives=%d found=%d, want 2/1", score.Positives, score.Found)
	}
	// MRR averaged over BOTH positives (the unranked one contributes 0): (1.0+0)/2.
	if math.Abs(score.MRR-0.5) > 1e-9 {
		t.Errorf("MRR = %v, want 0.5", score.MRR)
	}
	if score.Negatives != 1 || score.NegativeLeakK != 1 {
		t.Errorf("negatives=%d leak=%d, want 1/1", score.Negatives, score.NegativeLeakK)
	}
}

func TestReplayCorpus_NegativeBelowTopKDoesNotLeak(t *testing.T) {
	corpus := BehavioralCorpus{Entries: []BehavioralCorpusEntry{
		{Query: "q", ResultSignature: "bad", Polarity: -1},
	}}
	ranking := CandidateRanking{"q": {"a", "b", "c", "bad"}} // negative at rank 3
	score := ReplayCorpus(corpus, ranking, 3)
	if score.NegativeLeakK != 0 {
		t.Errorf("negative at rank 3 must not leak into top-3, got %d", score.NegativeLeakK)
	}
}
