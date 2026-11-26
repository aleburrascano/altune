package spellingmatch_test

import (
	"altune/go-api/internal/search/internal/adapters/cache/spellingmatch"
	"math"
	"slices"
	"testing"
)

func TestTrigrams(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty has none", in: "", want: nil},
		{name: "one rune is its own gram", in: "a", want: []string{"a"}},
		{name: "two runes are one gram", in: "ab", want: []string{"ab"}},
		{name: "three runes are one gram", in: "abc", want: []string{"abc"}},
		{name: "a word slides one rune at a time", in: "megaman", want: []string{"meg", "ega", "gam", "ama", "man"}},
		{name: "grams count runes, not bytes", in: "café", want: []string{"caf", "afé"}},
		{name: "a short word of multibyte runes is one gram", in: "éé", want: []string{"éé"}},
		{name: "a short word is kept exactly as given", in: "\xffa", want: []string{"\xffa"}},
		{name: "a three-rune word is rebuilt from its runes", in: "\xffab", want: []string{"\uFFFDab"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := spellingmatch.Trigrams(tt.in); !slices.Equal(got, tt.want) || (got == nil) != (tt.want == nil) {
				t.Errorf("Trigrams(%q) = %#v, want %#v", tt.in, got, tt.want)
			}
		})
	}
}

func TestQuery_KeepsTheQueryTrigrams(t *testing.T) {
	if got := spellingmatch.NewQuery("megaman").Trigrams(); !slices.Equal(got, []string{"meg", "ega", "gam", "ama", "man"}) {
		t.Errorf("Trigrams = %v, want the five grams of megaman", got)
	}
}

const scoreEpsilon = 1e-9

func TestQuery_ScoreWeighsTrigramsEditsPhoneticsAndLength(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		candidate string
		shared    int
		phonetic  bool
		want      float64
	}{
		{"identical terms", "cat", "cat", 1, false, 0.35*1 + 0.30*1 + 0.15*1},
		{"one edit with no shared gram", "cat", "car", 0, false, 0.30*(1-1.0/3) + 0.15*1},
		{"one extra rune", "cat", "cats", 1, false, 0.35*(1.0/2) + 0.30*(1-1.0/4) + 0.15*(1-1.0/4)},
		{"a phonetic match adds its weight", "kitten", "sitting", 1, true, 0.35*(1.0/8) + 0.30*(1-3.0/7) + 0.20 + 0.15*(1-1.0/7)},
		{"lengths count runes", "café", "cafe", 1, false, 0.35*(1.0/3) + 0.30*(1-1.0/4) + 0.15},
		{"two empty terms score zero", "", "", 0, false, 0},
		{"an empty query against one rune scores zero", "", "a", 0, false, 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, matched := spellingmatch.NewQuery(tt.query).Score(tt.candidate, tt.shared, tt.phonetic)
			if !matched {
				t.Fatalf("Score(%q vs %q) did not match", tt.query, tt.candidate)
			}
			if math.Abs(got-tt.want) > scoreEpsilon {
				t.Errorf("Score(%q vs %q) = %v, want %v", tt.query, tt.candidate, got, tt.want)
			}
		})
	}
}

func TestQuery_ScoreToleratesMoreEditsForLongerQueries(t *testing.T) {
	tests := []struct {
		name      string
		query     string
		within    string
		beyond    string
		tolerance int
	}{
		{"an empty query", "", "a", "ab", 1},
		{"four runes", "abcd", "abcx", "abxx", 1},
		{"five runes", "abcde", "abcxx", "abxxx", 2},
		{"eight runes", "abcdefgh", "abcdefxx", "abcdexxx", 2},
		{"nine runes", "abcdefghi", "abcdefxxx", "abcdexxxx", 3},
		{"four multibyte runes", "éééé", "éééx", "ééxx", 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			q := spellingmatch.NewQuery(tt.query)
			if _, matched := q.Score(tt.within, 0, false); !matched {
				t.Errorf("%q is %d edits from %q and was rejected, want a match", tt.within, tt.tolerance, tt.query)
			}
			if _, matched := q.Score(tt.beyond, 0, false); matched {
				t.Errorf("%q is %d edits from %q and matched, want it rejected", tt.beyond, tt.tolerance+1, tt.query)
			}
		})
	}
}

func TestQuery_APhoneticMatchSurvivesAnyEditDistance(t *testing.T) {
	q := spellingmatch.NewQuery("kitten")
	if _, matched := q.Score("sitting", 1, false); matched {
		t.Fatal("kitten vs sitting is 3 edits, beyond the 2 a six-rune query allows, want it rejected")
	}
	if _, matched := q.Score("sitting", 1, true); !matched {
		t.Error("a phonetic match was rejected for its edit distance")
	}
}
