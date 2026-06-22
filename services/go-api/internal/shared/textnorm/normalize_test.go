package textnorm

import (
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
