package textnorm

import (
	"math"
	"testing"
)

func TestTokenSortRatio(t *testing.T) {
	tests := []struct {
		name    string
		s1      string
		s2      string
		wantMin float64
		wantMax float64
	}{
		{
			name:    "identical strings",
			s1:      "hello world",
			s2:      "hello world",
			wantMin: 100,
			wantMax: 100,
		},
		{
			name:    "same tokens different order",
			s1:      "world hello",
			s2:      "hello world",
			wantMin: 100,
			wantMax: 100,
		},
		{
			name:    "partial overlap",
			s1:      "hello world foo",
			s2:      "hello world bar",
			wantMin: 70,
			wantMax: 95,
		},
		{
			name:    "no match",
			s1:      "aaa",
			s2:      "zzz",
			wantMin: 0,
			wantMax: 10,
		},
		{
			name:    "both empty",
			s1:      "",
			s2:      "",
			wantMin: 100,
			wantMax: 100,
		},
		{
			name:    "one empty",
			s1:      "hello",
			s2:      "",
			wantMin: 0,
			wantMax: 0,
		},
		{
			name:    "case insensitive",
			s1:      "Hello World",
			s2:      "hello world",
			wantMin: 100,
			wantMax: 100,
		},
		{
			name:    "subset tokens",
			s1:      "alpha beta gamma",
			s2:      "alpha beta",
			wantMin: 50,
			wantMax: 80,
		},
		{
			name:    "completely different",
			s1:      "abcdef",
			s2:      "xyz",
			wantMin: 0,
			wantMax: 10,
		},
		{
			// Rune-counted total: CJK strings sharing zero characters must score
			// 0, not the ~67 a byte-length total produced (3 bytes per rune).
			name:    "cjk no shared characters",
			s1:      "坂本",
			s2:      "龍一",
			wantMin: 0,
			wantMax: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TokenSortRatio(tt.s1, tt.s2)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("TokenSortRatio(%q, %q) = %.2f, want [%.0f, %.0f]",
					tt.s1, tt.s2, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"megaman", "megamsn", 1},
		{"kitten", "sitting", 3},
		{"same", "same", 0},
		// Rune-based, not byte-based: a single accented character must diff as
		// one edit, not the multi-byte UTF-8 sequence it's encoded as.
		{"beyonce", "beyoncé", 1},
	}
	for _, tt := range tests {
		t.Run(tt.a+"_"+tt.b, func(t *testing.T) {
			got := LevenshteinDistance(tt.a, tt.b)
			if got != tt.want {
				t.Errorf("LevenshteinDistance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestTokenSortRatio_Symmetry(t *testing.T) {
	pairs := [][2]string{
		{"hello world", "world hello"},
		{"foo bar baz", "baz foo"},
		{"alpha", "beta"},
	}

	for _, p := range pairs {
		t.Run(p[0]+"_vs_"+p[1], func(t *testing.T) {
			ab := TokenSortRatio(p[0], p[1])
			ba := TokenSortRatio(p[1], p[0])
			if math.Abs(ab-ba) > 0.01 {
				t.Errorf("TokenSortRatio is not symmetric: (%q,%q)=%.2f but (%q,%q)=%.2f",
					p[0], p[1], ab, p[1], p[0], ba)
			}
		})
	}
}
