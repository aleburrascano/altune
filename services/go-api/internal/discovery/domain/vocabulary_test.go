package domain

import (
	"strings"
	"testing"
)

func TestIsIndexableVocabularyTerm(t *testing.T) {
	atCap := strings.Repeat("é", MaxVocabularyTermRunes)
	over := atCap + "x"
	cases := []struct {
		term, norm string
		want       bool
	}{
		{"Kendrick Lamar", "kendrick lamar", true},
		{atCap, atCap, true},
		{over, "short", false},
		{"short", over, false},
	}
	for _, c := range cases {
		if got := IsIndexableVocabularyTerm(c.term, c.norm); got != c.want {
			t.Errorf("IsIndexableVocabularyTerm(len %d, len %d) = %v, want %v",
				len([]rune(c.term)), len([]rune(c.norm)), got, c.want)
		}
	}
}
