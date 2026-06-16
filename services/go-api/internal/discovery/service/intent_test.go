package service

import (
	"context"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

type mockVocabForIntent struct {
	entries map[string][]domain.VocabularyEntry
}

func (m *mockVocabForIntent) Add(_ context.Context, _ domain.VocabularyEntry) error { return nil }
func (m *mockVocabForIntent) BulkAdd(_ context.Context, _ []domain.VocabularyEntry) error {
	return nil
}
func (m *mockVocabForIntent) FindClosest(_ context.Context, _ string, _ int) ([]domain.VocabularyEntry, error) {
	return nil, nil
}
func (m *mockVocabForIntent) SuggestByPrefix(_ context.Context, prefix string, limit int) ([]domain.VocabularyEntry, error) {
	entries := m.entries[prefix]
	if len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func TestDetectIntent(t *testing.T) {
	vocab := &mockVocabForIntent{
		entries: map[string][]domain.VocabularyEntry{
			"tay k": {{Term: "Tay-K", TermNorm: "tay k", Kind: "artist", Popularity: 80}},
		},
	}

	t.Run("detects artist+track", func(t *testing.T) {
		intent := DetectIntent(context.Background(), "tay-k megaman", vocab)
		if intent == nil {
			t.Fatal("expected intent, got nil")
		}
		if intent.Artist != "tay-k" {
			t.Errorf("expected artist 'tay-k', got %q", intent.Artist)
		}
		if intent.Track != "megaman" {
			t.Errorf("expected track 'megaman', got %q", intent.Track)
		}
	})

	t.Run("detects track+artist order", func(t *testing.T) {
		intent := DetectIntent(context.Background(), "megaman tay-k", vocab)
		if intent == nil {
			t.Fatal("expected intent, got nil")
		}
		if intent.Artist != "tay-k" {
			t.Errorf("expected artist 'tay-k', got %q", intent.Artist)
		}
	})

	t.Run("single term returns nil", func(t *testing.T) {
		intent := DetectIntent(context.Background(), "megaman", vocab)
		if intent != nil {
			t.Errorf("expected nil for single term, got %+v", intent)
		}
	})

	t.Run("no artist match returns nil", func(t *testing.T) {
		intent := DetectIntent(context.Background(), "unknown artist somesong", vocab)
		if intent != nil {
			t.Errorf("expected nil when no artist match, got %+v", intent)
		}
	})

	t.Run("nil vocab returns nil", func(t *testing.T) {
		intent := DetectIntent(context.Background(), "tay-k megaman", nil)
		if intent != nil {
			t.Errorf("expected nil with nil vocab, got %+v", intent)
		}
	})
}

func TestApplyIntentBoost(t *testing.T) {
	intent := &QueryIntent{Artist: "Tay-K", Track: "Megaman"}

	t.Run("matching result gets boosted", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Megaman", "Tay-K", map[string]any{"popularity": int64(50)}),
		}
		got := ApplyIntentBoost(results, intent)
		pop := popularity(got[0])
		if pop <= 50 {
			t.Errorf("expected boosted popularity > 50, got %v", pop)
		}
	})

	t.Run("non-matching result unchanged", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Other Song", "Other Artist", map[string]any{"popularity": int64(50)}),
		}
		got := ApplyIntentBoost(results, intent)
		pop := popularity(got[0])
		if pop != 50 {
			t.Errorf("expected unchanged popularity 50, got %v", pop)
		}
	})

	t.Run("nil intent no-ops", func(t *testing.T) {
		results := []domain.SearchResult{
			trackResult(domain.ProviderDeezer, "1", "Megaman", "Tay-K", map[string]any{"popularity": int64(50)}),
		}
		got := ApplyIntentBoost(results, nil)
		pop := popularity(got[0])
		if pop != 50 {
			t.Errorf("expected unchanged popularity 50, got %v", pop)
		}
	})
}
