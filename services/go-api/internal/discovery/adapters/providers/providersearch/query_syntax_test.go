package providersearch

import "testing"

func TestMBLuceneEscape(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"leading wildcard", "*NSYNC", `\*NSYNC`},
		{"unbalanced parens", "Sunn O)))", `Sunn O\)\)\)`},
		{"trailing backslash", `Foo\`, `Foo\\`},
		{"embedded quote", `The "Best" Band`, `The \"Best\" Band`},
		{"double ampersand", "Salt && Pepa", `Salt \&\& Pepa`},
		{"double pipe", "Crosby || Nash", `Crosby \|\| Nash`},
		{"colon field-like", "genre:rock", `genre\:rock`},
		{"tilde fuzzy", "Wumpscut~", `Wumpscut\~`},
		{"caret boost", "Boost^2", `Boost\^2`},
		{"slash", "AC/DC", `AC\/DC`},
		{"brackets and braces", "[Alt] {Rock}", `\[Alt\] \{Rock\}`},
		{"plus minus bang", "+Required -Excluded !Not", `\+Required \-Excluded \!Not`},
		{"question mark", "Who?", `Who\?`},
		{"plain text unaffected", "Radiohead", "Radiohead"},
		{"empty string", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := MbLuceneEscape(tt.in); got != tt.want {
				t.Errorf("MbLuceneEscape(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

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
			if got := DslQuote(tt.phrase); got != tt.want {
				t.Errorf("DslQuote(%q) = %q, want %q", tt.phrase, got, tt.want)
			}
		})
	}
}

func TestParseListeners(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"1500000", 1500000},
		{"1,500,000", 1500000},
		{"", 0},
		{"n/a", 0},
	}
	for _, tt := range tests {
		if got := ParseListeners(tt.in); got != tt.want {
			t.Errorf("parseListeners(%q) = %d, want %d", tt.in, got, tt.want)
		}
	}
}
