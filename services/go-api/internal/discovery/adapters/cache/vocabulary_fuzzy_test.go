package cache

import (
	"math"
	"testing"

	"altune/go-api/internal/shared/textnorm"
)

const scoreEpsilon = 1e-9

func floatEq(a, b float64) bool { return math.Abs(a-b) < scoreEpsilon }

func TestJaccardCoefficientExactValues(t *testing.T) {
	cases := []struct {
		name                   string
		shared, totalA, totalB int
		want                   float64
	}{
		{"half overlap", 2, 3, 3, 0.5},
		{"no overlap", 0, 3, 3, 0.0},
		{"full overlap", 3, 3, 3, 1.0},
		{"empty union guards divide by zero", 0, 0, 0, 0.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := jaccardCoefficient(c.shared, c.totalA, c.totalB)
			if !floatEq(got, c.want) {
				t.Fatalf("jaccardCoefficient(%d,%d,%d) = %v, want %v", c.shared, c.totalA, c.totalB, got, c.want)
			}
		})
	}
}

func TestMaxLevenshtein(t *testing.T) {
	cases := []struct {
		query string
		want  int
	}{
		{"", 1},
		{"abcd", 1},
		{"abcde", 2},
		{"abcdefgh", 2},
		{"abcdefghi", 3},
	}
	for _, c := range cases {
		if got := maxLevenshtein(c.query); got != c.want {
			t.Errorf("maxLevenshtein(%q) = %d, want %d", c.query, got, c.want)
		}
	}
}

func TestLevenshteinSimilarity(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		dist int
		want float64
	}{
		{"identical", "cat", "cat", 0, 1.0},
		{"one edit of three", "cat", "car", 1, 1.0 - 1.0/3.0},
		{"both empty guards divide by zero", "", "", 0, 0.0},
		{"kitten sitting", "kitten", "sitting", 3, 1.0 - 3.0/7.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := levenshteinSimilarity(c.a, c.b, c.dist)
			if !floatEq(got, c.want) {
				t.Fatalf("levenshteinSimilarity(%q,%q,%d) = %v, want %v", c.a, c.b, c.dist, got, c.want)
			}
		})
	}
}

func TestLevenshteinSimilarityUsesActualDistance(t *testing.T) {
	a, b := "kitten", "sitting"
	dist := textnorm.LevenshteinDistance(a, b)
	if dist != 3 {
		t.Fatalf("LevenshteinDistance(%q,%q) = %d, want 3", a, b, dist)
	}
	want := 1.0 - 3.0/7.0
	if got := levenshteinSimilarity(a, b, dist); !floatEq(got, want) {
		t.Fatalf("levenshteinSimilarity with real distance = %v, want %v", got, want)
	}
}

func TestPhoneticScore(t *testing.T) {
	if got := phoneticScore(true); !floatEq(got, 1.0) {
		t.Errorf("phoneticScore(true) = %v, want 1.0", got)
	}
	if got := phoneticScore(false); !floatEq(got, 0.0) {
		t.Errorf("phoneticScore(false) = %v, want 0.0", got)
	}
}

func TestLengthSimilarity(t *testing.T) {
	cases := []struct {
		name string
		a, b string
		want float64
	}{
		{"equal length", "cat", "dog", 1.0},
		{"off by one", "cat", "cats", 0.75},
		{"one empty", "", "abc", 0.0},
		{"both empty guards divide by zero", "", "", 0.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := lengthSimilarity(c.a, c.b)
			if !floatEq(got, c.want) {
				t.Fatalf("lengthSimilarity(%q,%q) = %v, want %v", c.a, c.b, got, c.want)
			}
		})
	}
}
