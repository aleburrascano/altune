package versiontag

import (
	"slices"
	"strings"
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

func TestWithoutVersionPhrases_KeepsTitleWords(t *testing.T) {
	cases := map[string]string{
		"Devil In A New Dress":      "devil in a new dress",
		"Cake By The Ocean":         "cake by the ocean",
		"Put On A Smile":            "put on a smile",
		"Solo (Reprise)":            "solo reprise",
		"Human (2003 Remastered)":   "human 2003",
		"Cold Heart (PNAU Remix)":   "cold heart pnau",
		"Hold Up (Sped Up Version)": "hold up",
		"Pt. 2 (Live in the Booth)": "pt 2",
	}
	for title, want := range cases {
		if got := strings.Join(WithoutVersionPhrases(Tokens(title)), " "); got != want {
			t.Errorf("WithoutVersionPhrases(%q) = %q, want %q", title, got, want)
		}
	}
}
