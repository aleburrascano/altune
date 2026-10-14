package versiontag

import (
	"slices"
	"testing"
)

func TestLabels_RealLibraryTitles(t *testing.T) {
	cases := map[string][]string{
		"Human (Remastered 2003)":               {"remaster"},
		"like JENNIE (Extended Remix)":          {"extended", "remix"},
		`1999 (7" Stereo Edit - 2019 Remaster)`: {"edit", "remaster"},
		"I Ran (So Far Away) (Re-Recorded)":     {"re-recorded"},
		"Solo (Reprise)":                        nil,
		"Syrup Sandwiches (feat. Joey Bada$$)":  nil,
		"Lady (Hear Me Tonight) - Slowed":       {"slowed"},
		"Heaven (Classic Version)":              {"version"},
		"Never Let Me Down (2019 Mix)":          nil,
	}
	for title, want := range cases {
		if got := Labels(title); !slices.Equal(got, want) {
			t.Errorf("Labels(%q) = %v, want %v", title, got, want)
		}
	}
}

func TestIsVersionWord_KeepsTitleWords(t *testing.T) {
	for _, word := range []string{"reprise", "interlude", "pt", "away"} {
		if IsVersionWord(word) {
			t.Errorf("IsVersionWord(%q) = true, want false: it belongs to the title", word)
		}
	}
	for _, word := range []string{"remix", "remastered", "live", "extended"} {
		if !IsVersionWord(word) {
			t.Errorf("IsVersionWord(%q) = false, want true", word)
		}
	}
}
