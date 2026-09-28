package providers

import "testing"

func TestDslQuote(t *testing.T) {
	tests := []struct {
		name   string
		phrase string
		want   string
	}{
		{"plain phrase", "a b", `"a b"`},
		{"backslash untouched", `a\b`, `"a\b"`},
		{"tab escape text untouched", `a\tb`, `"a\tb"`},
		{"non-ascii untouched", "Björk – 東京", `"Björk – 東京"`},
		{"empty", "", `""`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := dslQuote(tt.phrase); got != tt.want {
				t.Errorf("dslQuote(%q) = %q, want %q", tt.phrase, got, tt.want)
			}
		})
	}
}
