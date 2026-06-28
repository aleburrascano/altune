package textnorm

import (
	"regexp"
	"testing"
)

func TestNormalizeForMatch(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "lowercase",
			input: "RADIOHEAD",
			want:  "radiohead",
		},
		{
			name:  "unicode diacritics stripped",
			input: "café",
			want:  "cafe",
		},
		{
			name:  "precomposed diacritics stripped",
			input: "café",
			want:  "cafe",
		},
		{
			name:  "brackets stripped - parentheses",
			input: "Song (Deluxe Edition)",
			want:  "song",
		},
		{
			name:  "brackets stripped - square",
			input: "Song [Remaster]",
			want:  "song",
		},
		{
			name:  "feat dot normalized",
			input: "Song feat. Artist",
			want:  "song feat artist",
		},
		{
			name:  "ft no longer normalized to feat",
			input: "Song ft. Artist",
			want:  "song ft artist",
		},
		{
			name:  "featuring no longer normalized to feat",
			input: "Song featuring Artist",
			want:  "song featuring artist",
		},
		{
			name:  "leading article the kept",
			input: "The Beatles",
			want:  "the beatles",
		},
		{
			name:  "ampersand to and",
			input: "Simon & Garfunkel",
			want:  "simon and garfunkel",
		},
		{
			name:  "punctuation stripped",
			input: "rock'n'roll!",
			want:  "rocknroll",
		},
		{
			name:  "whitespace collapsed",
			input: "  lots   of    spaces  ",
			want:  "lots of spaces",
		},
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "combined normalization keeps leading article",
			input: "The Café (Remastered) feat. DJ & MC",
			want:  "the cafe feat dj and mc",
		},
		{
			name:  "leading article la kept",
			input: "La Bamba",
			want:  "la bamba",
		},
		{
			name:  "leading article le kept",
			input: "Le Freak",
			want:  "le freak",
		},
		{
			name:  "apostrophes stripped",
			input: "don't stop",
			want:  "dont stop",
		},
		{
			name:  "periods stripped",
			input: "Dr. Dre",
			want:  "dr dre",
		},
		{
			name:  "commas stripped",
			input: "Crosby, Stills, Nash",
			want:  "crosby stills nash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeForMatch(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeForMatch(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestNormalizeForMatchNonLatin pins the CJK fix: non-Latin letters now survive
// (the query is rankable) while symbols/hyphens are still stripped, and matching
// stays symmetric.
func TestNormalizeForMatchNonLatin(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "japanese kanji preserved (was empty)",
			input: "坂本龍一",
			want:  "坂本龍一",
		},
		{
			name:  "japanese mixed kanji-katakana preserved",
			input: "宇多田ヒカル",
			want:  "宇多田ヒカル",
		},
		{
			name:  "korean hangul preserved",
			input: "뉴진스",
			want:  "뉴진스",
		},
		{
			name:  "cyrillic preserved",
			input: "Молчат Дома",
			want:  "молчат дома",
		},
		{
			name:  "non-decomposable latin letter preserved",
			input: "Sæglópur",
			want:  "sæglopur",
		},
		{
			name:  "cjk with latin and symbols",
			input: "m-flo loves 坂本龍一",
			want:  "m flo loves 坂本龍一",
		},
		{
			name:  "symbols still stripped",
			input: "¥$",
			want:  "",
		},
		{
			name:  "hyphen still becomes space",
			input: "blink-182",
			want:  "blink 182",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := NormalizeForMatch(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeForMatch(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

// TestStripSymbolsASCIIByteIdentical guards the load-bearing safety property: for
// ASCII input, stripSymbols must produce exactly what the old `[^\w\s]` regex did
// (ASCII alphanumerics and `_` kept, every other byte → space). If this drifts, the
// CJK change is no longer additive-only and could perturb the Latin eval corpus.
func TestStripSymbolsASCIIByteIdentical(t *testing.T) {
	oldRe := regexp.MustCompile(`[^\w\s]`)
	for b := 0; b < 128; b++ {
		in := string(rune(b)) // single ASCII byte
		got := stripSymbols(in)
		want := oldRe.ReplaceAllString(in, " ")
		if got != want {
			t.Errorf("stripSymbols(%q) = %q, old regex = %q", in, got, want)
		}
	}
}
