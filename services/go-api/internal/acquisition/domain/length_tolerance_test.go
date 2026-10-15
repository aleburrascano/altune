package domain

import (
	"math"
	"testing"
)

func TestLengthTolerance_NamedProfilesKeepTodaysWindows(t *testing.T) {
	tests := []struct {
		name          string
		profile       LengthTolerance
		expected      float64
		wantAllowance float64
		agrees        []float64
		disagrees     []float64
	}{
		{"tight", LengthTight, 300, 2, []float64{298, 302}, []float64{297.9, 302.1}},
		{"authoritative flat", LengthAuthoritative, 100, 5, []float64{95, 105}, []float64{94.9, 105.1}},
		{"authoritative fraction", LengthAuthoritative, 400, 12, []float64{388, 412}, []float64{387.9, 412.1}},
		{"loose flat", LengthLoose, 100, 15, []float64{85, 115}, []float64{84.9, 115.1}},
		{"loose fraction", LengthLoose, 400, 28, []float64{372, 428}, []float64{371.9, 428.1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.profile.Allowance(tt.expected); math.Abs(got-tt.wantAllowance) > 1e-9 {
				t.Errorf("Allowance(%v) = %v, want %v", tt.expected, got, tt.wantAllowance)
			}
			for _, a := range tt.agrees {
				if !tt.profile.Agrees(tt.expected, a) {
					t.Errorf("Agrees(%v, %v) = false, want true", tt.expected, a)
				}
			}
			for _, a := range tt.disagrees {
				if tt.profile.Agrees(tt.expected, a) {
					t.Errorf("Agrees(%v, %v) = true, want false", tt.expected, a)
				}
			}
		})
	}
}
