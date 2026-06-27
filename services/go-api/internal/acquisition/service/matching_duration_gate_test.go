package service

import "testing"

func TestDurationPlausible(t *testing.T) {
	tests := []struct {
		name             string
		expected, actual float64
		want             bool
	}{
		{name: "exact match", expected: 226, actual: 226, want: true},
		{name: "small absolute diff within slack", expected: 226, actual: 280, want: true},
		{name: "extended version within ratio", expected: 230, actual: 300, want: true},
		{name: "14-minute upload for a 3:46 track", expected: 226, actual: 840, want: false},
		{name: "hour-long loop", expected: 200, actual: 3600, want: false},
		{name: "30s preview for a full track", expected: 226, actual: 30, want: false},
		{name: "unknown expected does not gate", expected: 0, actual: 840, want: true},
		{name: "unknown actual does not gate", expected: 226, actual: 0, want: true},
		{name: "short track, small swing kept by slack", expected: 40, actual: 80, want: true},
		{name: "exactly 2x ratio kept", expected: 200, actual: 400, want: true},
		{name: "just over 2x ratio rejected", expected: 200, actual: 461, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := durationPlausible(tt.expected, tt.actual); got != tt.want {
				t.Errorf("durationPlausible(%v, %v) = %v, want %v", tt.expected, tt.actual, got, tt.want)
			}
		})
	}
}

// TestSelectBestCandidate_RejectsGrossDurationMismatch reproduces the "How Sweet"
// bug: a 14-minute upload (a mix/loop/full-album entry) was selected for a 3:46
// track. With the duration gate it is rejected in both buckets, so a correctly
// sized candidate wins — and when the bloated upload is the only option, nothing
// is selected (the track fails and can be retried) rather than storing a 14:00
// file.
func TestSelectBestCandidate_RejectsGrossDurationMismatch(t *testing.T) {
	track := TrackRef{Title: "How Sweet", Artist: "NewJeans", Duration: 226}

	t.Run("correct-length candidate wins over a 14-minute topic upload", func(t *testing.T) {
		candidates := []Candidate{
			{
				Title:      "How Sweet",
				Channel:    "NewJeans - Topic",
				Duration:   840, // 14:00 — a full-EP / mix upload
				URL:        "https://youtube.com/watch?v=bloated",
				Categories: []string{"Music"},
				ViewCount:  9_000_000,
			},
			{
				Title:      "How Sweet",
				Channel:    "HALLYUSOUND",
				Duration:   227,
				URL:        "https://youtube.com/watch?v=correct",
				Categories: []string{"Music"},
				ViewCount:  100_000,
			},
		}
		got := SelectBestCandidate(track, candidates)
		if got == nil {
			t.Fatal("expected the correct-length candidate, got nil")
		}
		if got.URL != "https://youtube.com/watch?v=correct" {
			t.Fatalf("selected a wrong-length candidate: %q (%.0fs)", got.URL, got.Duration)
		}
	})

	t.Run("a lone 14-minute upload is rejected rather than stored", func(t *testing.T) {
		candidates := []Candidate{
			{
				Title:      "How Sweet",
				Channel:    "NewJeans - Topic",
				Duration:   840,
				URL:        "https://youtube.com/watch?v=bloated",
				Categories: []string{"Music"},
				ViewCount:  9_000_000,
			},
		}
		if got := SelectBestCandidate(track, candidates); got != nil {
			t.Fatalf("expected nil (gross duration mismatch), got %q (%.0fs)", got.URL, got.Duration)
		}
	})
}
