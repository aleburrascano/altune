package domain_test

import (
	"altune/go-api/internal/download"
	"slices"
	"testing"
)

func TestSplitCredit(t *testing.T) {
	cases := map[string][]string{
		"A & B":            {"A", "B"},
		"A, B feat. C":     {"A", "B", "C"},
		"A ft. B":          {"A", "B"},
		"Mumford and Sons": {"Mumford and Sons"},
		"A featuring B":    {"A", "B"},
		" , & ":            {},
		"Beyoncé":          {"Beyoncé"},
	}
	for in, want := range cases {
		if got := download.SplitCredit(in); !slices.Equal(got, want) {
			t.Errorf("SplitCredit(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestFeaturedCredits(t *testing.T) {
	if got := download.FeaturedCredits("Song (feat. Beyoncé & JAY-Z)"); !slices.Equal(got, []string{"Beyoncé", "JAY-Z"}) {
		t.Errorf("got %q", got)
	}
	if got := download.FeaturedCredits("Song"); got != nil {
		t.Errorf("got %q, want nil", got)
	}
}

func TestCreditBrackets_StripsFeaturingLikeFeat(t *testing.T) {
	for _, in := range []string{"Song (featuring X)", "Song [ft. X]", "Song (with X)", "Song (bonus)"} {
		if got := download.CreditBrackets.ReplaceAllString(in, ""); got != "Song" {
			t.Errorf("%q -> %q", in, got)
		}
	}
}
