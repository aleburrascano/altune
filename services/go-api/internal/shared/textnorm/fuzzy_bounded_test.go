package textnorm

import (
	"strings"
	"testing"
	"time"
)

func TestFuzzyBoundedForHugeInput(t *testing.T) {
	huge := strings.Repeat("a", 900_000)
	other := strings.Repeat("b", 900_000)
	start := time.Now()
	d := LevenshteinDistance(huge, other)
	r := TokenSortRatio(huge, other)
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("took %v", el)
	}
	if d != maxFuzzyRunes {
		t.Fatalf("distance %d", d)
	}
	if r != 0 {
		t.Fatalf("ratio %v", r)
	}
}
