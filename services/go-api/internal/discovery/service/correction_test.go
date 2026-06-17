package service

import (
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func TestPickBestCorrection(t *testing.T) {
	t.Run("picks closest by edit distance", func(t *testing.T) {
		candidates := []domain.VocabularyEntry{
			{Term: "megaman", TermNorm: "megaman", Kind: "track", MatchScore: 0.72},
			{Term: "madonna", TermNorm: "madonna", Kind: "artist", MatchScore: 0.30},
		}
		result := pickBestCorrection("megamsn", candidates)
		if result == nil {
			t.Fatal("expected a correction result")
		}
		if result.Corrected != "megaman" {
			t.Errorf("expected corrected='megaman', got %q", result.Corrected)
		}
	})

	t.Run("rejects candidates beyond max edit distance", func(t *testing.T) {
		candidates := []domain.VocabularyEntry{
			{Term: "zzzzzzz", TermNorm: "zzzzzzz", Kind: "track", MatchScore: 0.80},
			{Term: "yyyyyyy", TermNorm: "yyyyyyy", Kind: "track", MatchScore: 0.90},
		}
		result := pickBestCorrection("megamsn", candidates)
		if result != nil {
			t.Errorf("expected nil for high-distance candidates, got %+v", result)
		}
	})

	t.Run("empty candidates returns nil", func(t *testing.T) {
		result := pickBestCorrection("megamsn", nil)
		if result != nil {
			t.Errorf("expected nil, got %+v", result)
		}
	})

	t.Run("same distance breaks tie by match score", func(t *testing.T) {
		candidates := []domain.VocabularyEntry{
			{Term: "weekand", TermNorm: "weekand", Kind: "track", MatchScore: 0.45},
			{Term: "weekynd", TermNorm: "weekynd", Kind: "artist", MatchScore: 0.66},
		}
		result := pickBestCorrection("weekend", candidates)
		if result == nil {
			t.Fatal("expected a correction result")
		}
		if result.Corrected != "weekynd" {
			t.Errorf("expected higher-scored tiebreaker 'weekynd', got %q", result.Corrected)
		}
	})

	t.Run("prefers lower edit distance over higher score", func(t *testing.T) {
		candidates := []domain.VocabularyEntry{
			{Term: "megaman", TermNorm: "megaman", Kind: "track", MatchScore: 0.50},
			{Term: "megazan", TermNorm: "megazan", Kind: "track", MatchScore: 0.90},
		}
		result := pickBestCorrection("megamsn", candidates)
		if result == nil {
			t.Fatal("expected a correction result")
		}
		if result.Corrected != "megaman" {
			t.Errorf("expected closer 'megaman' (dist 1) over 'megazan' (dist 2), got %q", result.Corrected)
		}
	})
}
