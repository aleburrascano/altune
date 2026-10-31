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

func TestWithoutRerecordingPhrases(t *testing.T) {
	cases := []struct {
		title string
		want  string
		found bool
	}{
		{"It’s Only Love (Classic Version)", "its only love", true},
		{"Love Story (Taylor's Version)", "love story", true},
		{"Song (Re-Recorded)", "song", true},
		{"Song (Rerecorded)", "song", true},
		{"Song (Re-Recording)", "song", true},
		{"Classic Rock Version Of Me", "classic rock version of me", false},
		{"Cold Heart (PNAU Remix)", "cold heart pnau remix", false},
	}
	for _, c := range cases {
		kept, found := WithoutRerecordingPhrases(Tokens(c.title))
		if got := strings.Join(kept, " "); got != c.want || found != c.found {
			t.Errorf("WithoutRerecordingPhrases(%q) = %q, %v; want %q, %v", c.title, got, found, c.want, c.found)
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

func TestParseTags_PartAndSequelNumbersAreData(t *testing.T) {
	cases := map[string]int{
		"Nun To Me":              1,
		"Nun To Me 2":            2,
		"Nun To Me, Pt. 2":       2,
		"Nun To Me Part II":      2,
		"Nun To Me II":           2,
		"Nun To Me Vol. 3":       3,
		"Nun To Me Part 1":       1,
		"Rocky Part V":           5,
		"Blink 182":              1,
		"Song 2 (Live)":          2,
		"Song 2 (feat. Someone)": 2,
		"2":                      1,
		"Malcolm X":              1,
	}
	for title, want := range cases {
		if got := ParseTags(title).Part; got != want {
			t.Errorf("ParseTags(%q).Part = %d, want %d", title, got, want)
		}
	}
}

func TestParseTags_VersionNumberIsALabelNotAPart(t *testing.T) {
	cases := []struct {
		title     string
		wantPart  int
		wantLabel string
	}{
		{"Song (Version 2)", 1, "v2"},
		{"Song [V2]", 1, "v2"},
		{"Song v.2", 1, "v2"},
		{"Song (Ver. 3)", 1, "v3"},
		{"Song Part 2 (Version 2)", 2, "v2"},
	}
	for _, tc := range cases {
		got := ParseTags(tc.title)
		if got.Part != tc.wantPart || !slices.Equal(got.Labels, []string{tc.wantLabel}) {
			t.Errorf("ParseTags(%q) = %+v, want part %d and label %q", tc.title, got, tc.wantPart, tc.wantLabel)
		}
	}
}

func TestTags_EqualReadsTwoVersion2TitlesAsTheSameVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Song [V2]", "Song (Version 2)", true},
		{"Song (Version 2)", "Song (Version 3)", false},
		{"Song (Version 2)", "Song", false},
		{"Song 2", "Song (Version 2)", false},
	}
	for _, tc := range cases {
		if got := ParseTags(tc.a).Equal(ParseTags(tc.b)); got != tc.want {
			t.Errorf("ParseTags(%q).Equal(ParseTags(%q)) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestTags_EqualComparesPartAndLabels(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"Song", "Song", true},
		{"Song", "Song (Remastered 2011)", true},
		{"Song", "Song 2", false},
		{"Song Part 1", "Song", true},
		{"Song Pt. 2", "Song Part II", true},
		{"Song", "Song (Live)", false},
		{"Song 2 (Live)", "Song 2", false},
	}
	for _, tc := range cases {
		if got := ParseTags(tc.a).Equal(ParseTags(tc.b)); got != tc.want {
			t.Errorf("ParseTags(%q).Equal(ParseTags(%q)) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestWithoutRerecordingPhrases_LoneWordsAndEmptyInputAreNotAPhrase(t *testing.T) {
	cases := []struct {
		title string
		want  string
	}{
		{"", ""},
		{"Classic", "classic"},
		{"Version", "version"},
		{"Classic Man", "classic man"},
		{"Classical Gas", "classical gas"},
		{"Version Classic", "version classic"},
		{"Taylors", "taylors"},
		{"Song (Album Version)", "song album version"},
	}
	for _, c := range cases {
		kept, found := WithoutRerecordingPhrases(Tokens(c.title))
		if got := strings.Join(kept, " "); got != c.want || found {
			t.Errorf("WithoutRerecordingPhrases(%q) = %q, %v; want %q, false", c.title, got, found, c.want)
		}
	}
}

func TestBaseTitle_SplitsTheVersionTagFromTheTitle(t *testing.T) {
	for _, c := range []struct{ title, base, label string }{
		{"Sports Car [V10]", "Sports Car", "V10"},
		{"Greenlight [V1] (Voice Memo)", "Greenlight", "V1 Voice Memo"},
		{"Signs", "Signs", ""},
		{"Song (feat. Bob) [V2]", "Song (feat. Bob)", "V2"},
	} {
		base, label := BaseTitle(c.title)
		if base != c.base || label != c.label {
			t.Errorf("BaseTitle(%q) = %q, %q; want %q, %q", c.title, base, label, c.base, c.label)
		}
	}
}

func TestWithoutRerecordingPhrases_FoldsCaseAndCurlyApostrophe(t *testing.T) {
	for title, want := range map[string]string{
		"Love Story (Taylor’s Version)": "love story",
		"LOVE STORY (TAYLOR'S VERSION)": "love story",
		"Classic Version":               "",
		"Song (Classic Version) [Live]": "song live",
		"Song - Re-Recorded Version":    "song version",
	} {
		kept, found := WithoutRerecordingPhrases(Tokens(title))
		if got := strings.Join(kept, " "); got != want || !found {
			t.Errorf("WithoutRerecordingPhrases(%q) = %q, %v; want %q, true", title, got, found, want)
		}
	}
}
