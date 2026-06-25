package eval

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/service"
	"altune/go-api/internal/shared/textnorm"
)

// vocabCorrector knows a fixed vocabulary and corrects any input within
// Levenshtein distance 1 of a known term back to it. Deterministic, no I/O —
// stands in for the real CorrectionService + vocab store.
type vocabCorrector struct {
	vocab          []string
	corruptOnValid bool // when true, "corrects" even an exact valid term (precision break)
}

func (f *vocabCorrector) nearest(query string) *service.CorrectionResult {
	q := textnorm.NormalizeForMatch(query)
	for _, term := range f.vocab {
		tn := textnorm.NormalizeForMatch(term)
		if tn == q {
			if f.corruptOnValid {
				return &service.CorrectionResult{Corrected: term + " x", Confidence: 0.5}
			}
			return nil // exact match — nothing to correct
		}
		if textnorm.LevenshteinDistance(q, tn) == 1 {
			return &service.CorrectionResult{Corrected: term, Confidence: 0.9}
		}
	}
	return nil
}

func (f *vocabCorrector) Correct(_ context.Context, q string) *service.CorrectionResult {
	return f.nearest(q)
}
func (f *vocabCorrector) CorrectAggressive(_ context.Context, q string) *service.CorrectionResult {
	return f.nearest(q)
}

func TestRunCorrectionEval_recallAndPrecision(t *testing.T) {
	terms := []string{"kendrick", "humble", "scorpion"}
	c := &vocabCorrector{vocab: terms}
	r := RunCorrectionEval(context.Background(), terms, c, 3)

	if r.Terms != 3 {
		t.Fatalf("expected 3 terms, got %d", r.Terms)
	}
	if r.TyposTested == 0 {
		t.Fatal("expected typos to be generated")
	}
	// Every distance-1 typo of a known term must be recovered by this fake.
	if r.RecallRate() != 1.0 {
		t.Errorf("recall_rate = %v, want 1.0 (misses: %+v)", r.RecallRate(), r.RecallMisses)
	}
	// No valid term should be corrupted.
	if r.PrecisionRate() != 1.0 {
		t.Errorf("precision_rate = %v, want 1.0 (corruptions: %+v)", r.PrecisionRate(), r.Corruptions)
	}
}

func TestRunCorrectionEval_detectsCorruption(t *testing.T) {
	terms := []string{"bohemian rhapsody"}
	c := &vocabCorrector{vocab: terms, corruptOnValid: true}
	r := RunCorrectionEval(context.Background(), terms, c, 2)

	if r.Corrupted != 1 {
		t.Errorf("expected 1 corruption, got %d", r.Corrupted)
	}
	if r.PrecisionRate() != 0 {
		t.Errorf("precision_rate = %v, want 0", r.PrecisionRate())
	}
	if len(c.vocab) == 0 || len(r.Corruptions) != 1 || r.Corruptions[0].Reason != "corrupted_valid_query" {
		t.Errorf("expected one attributed corruption record, got %+v", r.Corruptions)
	}
}

func TestSyntheticTypos_deterministicAndDistance1(t *testing.T) {
	a := syntheticTypos("kendrick", 3)
	b := syntheticTypos("kendrick", 3)
	if len(a) == 0 {
		t.Fatal("expected typos for a normal term")
	}
	if len(a) != len(b) {
		t.Fatalf("non-deterministic count: %d vs %d", len(a), len(b))
	}
	for i := range a {
		if a[i] != b[i] {
			t.Fatalf("non-deterministic typo at %d: %q vs %q", i, a[i], b[i])
		}
		if a[i] == "kendrick" {
			t.Errorf("typo equals the term: %q", a[i])
		}
		if d := textnorm.LevenshteinDistance("kendrick", a[i]); d != 1 {
			t.Errorf("typo %q is distance %d, want 1", a[i], d)
		}
	}
	// A term too short to perturb yields nothing.
	if got := syntheticTypos("a", 3); got != nil {
		t.Errorf("expected no typos for single-letter term, got %v", got)
	}
}
