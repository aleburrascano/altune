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

func TestRecordingLabels_NameADifferentRecording(t *testing.T) {
	for title, want := range map[string]string{
		"Wanna Be Startin' Somethin' (Album Version)":                "",
		"Con te partirò (2016 Version)":                              "version",
		"Take On Me (2017 Acoustic)":                                 "acoustic",
		"Hotel California (2013 Remaster)":                           "",
		"Novacane (Explicit Version)":                                "",
		"Everybody Wants To Rule The World (Live From Franklin, TN)": "live",
	} {
		if got := strings.Join(RecordingLabels(title), " "); got != want {
			t.Errorf("RecordingLabels(%q) = %q, want %q", title, got, want)
		}
	}
}

func TestMatches_ReleaseVariantsCarryTheirVeto(t *testing.T) {
	cases := map[string]struct {
		label string
		veto  bool
	}{
		"Song (Acoustic)":      {"acoustic", true},
		"Song (Demo)":          {"demo", true},
		"Song (Re-Recorded)":   {"re-recorded", true},
		"Song (Rerecorded)":    {"re-recorded", true},
		"Song (Remastered)":    {"remaster", false},
		"Song (2011 Remaster)": {"remaster", false},
	}
	for title, want := range cases {
		var found bool
		for _, entry := range Matches(Tokens(title)) {
			if entry.Label == want.label {
				found = true
				if entry.Veto != want.veto {
					t.Errorf("Matches(%q) %s veto = %v, want %v", title, want.label, entry.Veto, want.veto)
				}
			}
		}
		if !found {
			t.Errorf("Matches(%q) has no %s entry", title, want.label)
		}
	}
}

func TestLabels_RecordingLabelsKeepReleaseVariants(t *testing.T) {
	if got := Labels("Song (Acoustic) (Demo) (Remastered)"); !slices.Equal(got, []string{"acoustic", "demo", "remaster"}) {
		t.Errorf("Labels = %v", got)
	}
	if got := RecordingLabels("Song (Acoustic) (Demo) (Remastered)"); !slices.Equal(got, []string{"acoustic", "demo"}) {
		t.Errorf("RecordingLabels = %v", got)
	}
}
