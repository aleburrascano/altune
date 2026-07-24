package providers

import "testing"

func TestStripITunesTypeSuffix(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"suffix stripped", "Fully Loaded - EP", "Fully Loaded"},
		{"single suffix stripped", "Bad - Single", "Bad"},
		{"mid-title match untouched", "Bad - Remix Album", "Bad - Remix Album"},
		{"no suffix untouched", "OK Computer", "OK Computer"},
		// Turkish İ lowercases to a longer byte sequence; slicing the original at
		// an index from the lowered copy used to mangle the title.
		{"multibyte case-fold untouched", "İSTANBUL - Single", "İSTANBUL - Single"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := stripITunesTypeSuffix(tt.in); got != tt.want {
				t.Errorf("stripITunesTypeSuffix(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
