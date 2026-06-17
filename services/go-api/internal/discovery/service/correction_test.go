package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestPickBestCorrection(t *testing.T) {
	t.Run("returns best match above threshold", func(t *testing.T) {
		candidates := []domain.VocabularyEntry{
			{Term: "megaman", TermNorm: "megaman", Kind: "track", MatchScore: 0.72},
			{Term: "madonna", TermNorm: "madonna", Kind: "artist", MatchScore: 0.30},
			{Term: "metallica", TermNorm: "metallica", Kind: "artist", MatchScore: 0.15},
		}
		result := pickBestCorrection("megamsn", candidates)
		if result == nil {
			t.Fatal("expected a correction result")
		}
		if result.Corrected != "megaman" {
			t.Errorf("expected corrected='megaman', got %q", result.Corrected)
		}
		if result.Confidence != 0.72 {
			t.Errorf("expected confidence=0.72, got %v", result.Confidence)
		}
	})

	t.Run("returns nil when no match above threshold", func(t *testing.T) {
		candidates := []domain.VocabularyEntry{
			{Term: "zzzzzzz", TermNorm: "zzzzzzz", Kind: "track", MatchScore: 0.10},
			{Term: "yyyyyyy", TermNorm: "yyyyyyy", Kind: "track", MatchScore: 0.05},
		}
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

	t.Run("picks highest score among above-threshold candidates", func(t *testing.T) {
		candidates := []domain.VocabularyEntry{
			{Term: "weekend", TermNorm: "weekend", Kind: "track", MatchScore: 0.45},
			{Term: "weeknd", TermNorm: "weeknd", Kind: "artist", MatchScore: 0.66},
		}
		result := pickBestCorrection("weekend", candidates)
		if result == nil {
			t.Fatal("expected a correction result")
		}
		if result.Corrected != "weeknd" {
			t.Errorf("expected corrected='weeknd', got %q", result.Corrected)
		}
	})
}
