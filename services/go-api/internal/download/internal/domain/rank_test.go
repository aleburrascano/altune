package domain_test

import (
	"altune/go-api/internal/download/internal/domain"
	"testing"
)

func TestBeats_AUserIsTheTopOfTheScale(t *testing.T) {
	if !domain.AUser.Beats(domain.A) || domain.A.Beats(domain.AUser) || domain.AUser.Beats(domain.AUser) {
		t.Error("A_user must beat A, and nothing beats A_user")
	}
	if !domain.AUser.IsKnown() || !domain.AUser.IsTop() || !domain.A.IsTop() || domain.B.IsTop() {
		t.Error("only A and A_user are top grades")
	}
}

func TestBeats(t *testing.T) {
	cases := []struct {
		name      string
		candidate domain.Grade
		current   domain.Grade
		want      bool
	}{
		{"A beats B", domain.A, domain.B, true},
		{"B beats C", domain.B, domain.C, true},
		{"C beats D", domain.C, domain.D, true},
		{"D beats F", domain.D, domain.F, true},
		{"A beats F", domain.A, domain.F, true},
		{"an equal grade does not beat itself", domain.C, domain.C, false},
		{"B does not beat A", domain.B, domain.A, false},
		{"F does not beat D", domain.F, domain.D, false},
		{"an unknown candidate beats nothing", domain.Grade("Z"), domain.F, false},
		{"nothing is beaten by an unknown current grade", domain.A, domain.Grade(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.candidate.Beats(tc.current); got != tc.want {
				t.Errorf("%q.Beats(%q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
			}
		})
	}
}
