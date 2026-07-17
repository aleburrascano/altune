package app

import (
	"testing"

	domain "altune/go-api/internal/discovery/domain"
)

func TestMatchPosition(t *testing.T) {
	results := []domain.SearchResult{
		{Title: "Bohemian Rhapsody", Subtitle: "Queen"},
		{Title: "Some Other Track", Subtitle: "Artist"},
		{Title: "Third", Subtitle: "Third Artist"},
		{Title: "HUMBLE.", Subtitle: "Kendrick Lamar"}, // 4th — outside top-3
	}

	tests := []struct {
		name   string
		expect string
		want   int
	}{
		{"hit at position 0 (case-insensitive)", "bohemian rhapsody", 0},
		{"hit via subtitle", "queen", 0},
		{"later match reports its position", "humble", 3},
		{"miss", "nonexistent track", -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := matchPosition(results, tt.expect); got != tt.want {
				t.Errorf("matchPosition(%q) = %d, want %d", tt.expect, got, tt.want)
			}
		})
	}
}
