package domain_test

import (
	"altune/go-api/internal/download/internal/domain"
	"testing"
)

func TestBeats_AUserIsTheTopOfTheScale(t *testing.T) {
	if !domain.GradeAUser.Beats(domain.GradeA) || domain.GradeA.Beats(domain.GradeAUser) || domain.GradeAUser.Beats(domain.GradeAUser) {
		t.Error("A_user must beat A, and nothing beats A_user")
	}
	if !domain.GradeAUser.IsKnown() || !domain.GradeAUser.IsTop() || !domain.GradeA.IsTop() || domain.GradeB.IsTop() {
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
		{"A beats B", domain.GradeA, domain.GradeB, true},
		{"B beats C", domain.GradeB, domain.GradeC, true},
		{"C beats D", domain.GradeC, domain.GradeD, true},
		{"D beats F", domain.GradeD, domain.GradeF, true},
		{"A beats F", domain.GradeA, domain.GradeF, true},
		{"an equal grade does not beat itself", domain.GradeC, domain.GradeC, false},
		{"B does not beat A", domain.GradeB, domain.GradeA, false},
		{"F does not beat D", domain.GradeF, domain.GradeD, false},
		{"an unknown candidate beats nothing", domain.Grade("Z"), domain.GradeF, false},
		{"nothing is beaten by an unknown current grade", domain.GradeA, domain.Grade(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.candidate.Beats(tc.current); got != tc.want {
				t.Errorf("%q.Beats(%q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
			}
		})
	}
}
