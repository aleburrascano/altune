package providers

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
			if got := mbLuceneEscape(tt.in); got != tt.want {
				t.Errorf("mbLuceneEscape(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
