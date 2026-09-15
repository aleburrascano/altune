package textnorm

import (
	"strings"
	"testing"
)

func TestFoldName_UnifiesUnicodeForms(t *testing.T) {
	cases := []struct{ name, a, b string }{
		{"NFC vs NFD e-acute", "Beyoncé", "Beyoncé"},
		{"fullwidth vs ASCII", "ＲＯＳＡＬＩＡ", "ROSALIA"},
		{"ligature vs letters", "ﬁve", "five"},
		{"no-break space vs space", "Daft Punk", "daft punk"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if FoldName(c.a) != FoldName(c.b) {
				t.Errorf("FoldName(%q) = %q, FoldName(%q) = %q, want equal", c.a, FoldName(c.a), c.b, FoldName(c.b))
			}
		})
	}
}

// TestFoldName_UnchangedForAlreadyNormalInput pins that the NFKC step leaves
// the fold of ASCII and NFC text without compatibility characters unchanged, so
// featured-artist keys persisted before NFKC was added still match.
func TestFoldName_UnchangedForAlreadyNormalInput(t *testing.T) {
	legacy := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	inputs := []string{
		"Guest One", "  Daft   Punk ", "Beyoncé", "Sigur Rós",
		"坂本龍一", "Björk", "AC/DC", "Ty Dolla $ign",
	}
	for _, s := range inputs {
		if got, want := FoldName(s), legacy(s); got != want {
			t.Errorf("FoldName(%q) = %q, want legacy fold %q", s, got, want)
		}
	}
}

func TestFoldName_KeepsDiacriticsAndPunctuation(t *testing.T) {
	if FoldName("Beyoncé") == FoldName("Beyonce") {
		t.Error("FoldName must not strip diacritics")
	}
	if got := FoldName("AC/DC"); got != "ac/dc" {
		t.Errorf("FoldName(AC/DC) = %q, want ac/dc", got)
	}
}
