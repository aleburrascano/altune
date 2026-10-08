package textnorm

import "testing"

func TestNFKC(t *testing.T) {
	if got := NFKC("ﬁ"); got != "fi" {
		t.Errorf("NFKC(ligature) = %q, want fi", got)
	}
	if got := NFKC(""); got != "" {
		t.Errorf("NFKC(\"\") = %q, want empty", got)
	}
}

func TestNFKCFoldsFullwidthLetters(t *testing.T) {
	if got := NFKC("ｆｕｌｌ"); got != "full" {
		t.Errorf("NFKC(fullwidth) = %q, want full", got)
	}
}

func TestNFKCKeepsCaseAndPunctuation(t *testing.T) {
	if got := NFKC("Slowed + Reverb"); got != "Slowed + Reverb" {
		t.Errorf("NFKC(\"Slowed + Reverb\") = %q, want it unchanged", got)
	}
}
