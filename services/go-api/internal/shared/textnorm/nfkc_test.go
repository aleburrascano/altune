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
