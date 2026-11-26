package domain_test

import (
	"altune/go-api/internal/download"
	"math"
	"testing"
)

func TestLengthTolerance_AnUnknownLengthNeverAgrees(t *testing.T) {
	profiles := []struct {
		name    string
		profile download.LengthMargin
	}{
		{"tight", download.LengthTight},
		{"loose", download.LengthLoose},
	}
	for _, p := range profiles {
		for _, pair := range [][2]float64{{10, 0}, {0, 10}, {0, 0}, {10, -1}} {
			if p.profile.Agrees(pair[0], pair[1]) {
				t.Errorf("%s: Agrees(%v, %v) = true, want false: an unknown length is never a match", p.name, pair[0], pair[1])
			}
		}
	}
}

func TestLengthTolerance_NamedProfilesKeepTodaysWindows(t *testing.T) {
	tests := []struct {
		name          string
		profile       download.LengthMargin
		expected      float64
		wantAllowance float64
		agrees        []float64
		disagrees     []float64
	}{
		{"tight", download.LengthTight, 300, 2, []float64{298, 302}, []float64{297.9, 302.1}},
		{"loose flat", download.LengthLoose, 100, 15, []float64{85, 115}, []float64{84.9, 115.1}},
		{"loose fraction", download.LengthLoose, 400, 28, []float64{372, 428}, []float64{371.9, 428.1}},
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
