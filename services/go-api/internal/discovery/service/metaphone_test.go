package service

import "testing"

func TestDoubleMetaphone(t *testing.T) {
	tests := []struct {
		input   string
		primary string
	}{
		{"weeknd", "AKNT"},
		{"weekend", "AKNT"},
		{"smith", "SM0"},
		{"schmidt", "SKMT"},
		{"phone", "FN"},
		{"knight", "NKT"},
		{"wright", "RKT"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			pri, _ := DoubleMetaphone(tt.input)
			if pri != tt.primary {
				t.Errorf("DoubleMetaphone(%q) primary = %q, want %q", tt.input, pri, tt.primary)
			}
		})
	}
}

func TestDoubleMetaphone_PhoneticEquivalence(t *testing.T) {
	pairs := []struct {
		a, b string
	}{
		{"weeknd", "weekend"},
		{"justin", "justen"},
		{"ariana", "arianna"},
		{"mac", "mack"},
	}
	for _, tt := range pairs {
		t.Run(tt.a+"_vs_"+tt.b, func(t *testing.T) {
			pA, _ := DoubleMetaphone(tt.a)
			pB, _ := DoubleMetaphone(tt.b)
			if pA != pB {
				t.Errorf("DoubleMetaphone(%q)=%q != DoubleMetaphone(%q)=%q — expected same code",
					tt.a, pA, tt.b, pB)
			}
		})
	}
}

func TestMetaphoneKey(t *testing.T) {
	tests := []struct {
		term string
		want string
	}{
		{"the weeknd", "0AKNT"},
		{"the weekend", "0AKNT"},
		{"tay k", "TK"},
		{"", ""},
	}
	for _, tt := range tests {
		t.Run(tt.term, func(t *testing.T) {
			got := MetaphoneKey(tt.term)
			if got != tt.want {
				t.Errorf("MetaphoneKey(%q) = %q, want %q", tt.term, got, tt.want)
			}
		})
	}
}
