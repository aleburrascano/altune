package app

import (
	"testing"

	domain "altune/go-api/internal/discovery/domain"
)

func TestTopKContains(t *testing.T) {
	results := []domain.SearchResult{
		{Title: "Bohemian Rhapsody", Subtitle: "Queen"},
		{Title: "Some Other Song", Subtitle: "Artist"},
		{Title: "Third", Subtitle: "Third Artist"},
		{Title: "HUMBLE.", Subtitle: "Kendrick Lamar"}, // 4th — outside top-3
	}

	tests := []struct {
		name   string
		expect string
		k      int
		want   bool
	}{
		{"hit at position 0 (case-insensitive)", "bohemian rhapsody", 3, true},
		{"hit via subtitle", "queen", 3, true},
		{"match outside top-k is not counted", "humble", 3, false},
		{"raising k includes the later match", "humble", 4, true},
		{"miss", "nonexistent track", 3, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := topKContains(results, tt.expect, tt.k); got != tt.want {
				t.Errorf("topKContains(%q, k=%d) = %v, want %v", tt.expect, tt.k, got, tt.want)
			}
		})
	}
}
