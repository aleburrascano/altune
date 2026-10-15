package domain

import "testing"

func TestCredits_OneKeyForEveryCreditCompare(t *testing.T) {
	cases := []struct {
		name     string
		credited []string
		artist   string
		want     bool
	}{
		{"dollar reads as s", []string{"A$AP Rocky"}, "ASAP Rocky", true},
		{"dollar on the track side", []string{"ASAP Rocky"}, "A$AP Rocky", true},
		{"accents fold", []string{"Beyoncé"}, "Beyonce", true},
		{"any credit matches", []string{"Other", "Beyoncé"}, "Beyonce", true},
		{"different artist", []string{"Other"}, "Beyonce", false},
		{"no credits", nil, "Beyonce", false},
		{"empty artist never credited", []string{""}, "", false},
		{"punctuation-only artist never credited", []string{"!!!"}, "!!!", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Credits(tc.credited, tc.artist); got != tc.want {
				t.Fatalf("Credits(%q, %q) = %v, want %v", tc.credited, tc.artist, got, tc.want)
			}
		})
	}
}
