package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestTrigramJaccard(t *testing.T) {
	tests := []struct {
		name     string
		a, b     string
		minScore float64
		maxScore float64
	}{
		{"identical", "megaman", "megaman", 1.0, 1.0},
		{"one char off", "megaman", "megamsn", 0.3, 0.8},
		{"completely different", "megaman", "xkqzwp", 0.0, 0.1},
		{"empty a", "", "megaman", 0.0, 0.0},
		{"empty b", "megaman", "", 0.0, 0.0},
		{"short strings", "ab", "ab", 1.0, 1.0},
		{"short vs long", "ab", "abcdef", 0.0, 0.5},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score := trigramJaccard(tt.a, tt.b)
			if score < tt.minScore || score > tt.maxScore {
				t.Errorf("trigramJaccard(%q, %q) = %v, want [%v, %v]",
					tt.a, tt.b, score, tt.minScore, tt.maxScore)
			}
		})
	}
}

func TestPickBestCorrection(t *testing.T) {
	t.Run("returns best match above threshold", func(t *testing.T) {
		candidates := buildVocabCandidates("megaman", "madonna", "metallica")
		result := pickBestCorrection("megamsn", candidates)
		if result == nil {
			t.Fatal("expected a correction result")
		}
		if result.Corrected != "megaman" {
			t.Errorf("expected corrected='megaman', got %q", result.Corrected)
		}
	})

	t.Run("returns nil when no match above threshold", func(t *testing.T) {
		candidates := buildVocabCandidates("zzzzzzz", "yyyyyyy")
		result := pickBestCorrection("megamsn", candidates)
		if result != nil {
			t.Errorf("expected nil, got %+v", result)
		}
	})

	t.Run("empty candidates returns nil", func(t *testing.T) {
		result := pickBestCorrection("megamsn", nil)
		if result != nil {
			t.Errorf("expected nil, got %+v", result)
		}
	})
}

func buildVocabCandidates(terms ...string) []domain.VocabularyEntry {
	entries := make([]domain.VocabularyEntry, len(terms))
	for i, term := range terms {
		entries[i] = domain.VocabularyEntry{
			Term:     term,
			TermNorm: term,
			Kind:     "track",
		}
	}
	return entries
}
