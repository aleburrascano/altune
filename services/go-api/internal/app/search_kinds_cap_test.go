package app

import (
	"strings"
	"testing"
)

func TestParseSearchKinds_InvalidListIsBounded(t *testing.T) {
	kinds := make([]string, 10000)
	for i := range kinds {
		kinds[i] = "x"
	}
	_, err := parseSearchKinds(kinds)
	if err == nil {
		t.Fatal("want error")
	}
	if n := strings.Count(err.Error(), "x"); n > maxInvalidKindsListed {
		t.Fatalf("error lists %d names, want at most %d", n, maxInvalidKindsListed)
	}
}
