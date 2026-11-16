package grade_test

import (
	"altune/go-api/internal/acquisition"
	"testing"
)

func TestBeats_AUserIsTheTopOfTheScale(t *testing.T) {
	if !acquisition.GradeAUser.Beats(acquisition.GradeA) || acquisition.GradeA.Beats(acquisition.GradeAUser) || acquisition.GradeAUser.Beats(acquisition.GradeAUser) {
		t.Error("A_user must beat A, and nothing beats A_user")
	}
	if !acquisition.GradeAUser.IsKnown() || !acquisition.GradeAUser.IsTop() || !acquisition.GradeA.IsTop() || acquisition.GradeB.IsTop() {
		t.Error("only A and A_user are top grades")
	}
}

func TestBeats(t *testing.T) {
	cases := []struct {
		name      string
		candidate acquisition.Grade
		current   acquisition.Grade
		want      bool
	}{
		{"A beats B", acquisition.GradeA, acquisition.GradeB, true},
		{"B beats C", acquisition.GradeB, acquisition.GradeC, true},
		{"C beats D", acquisition.GradeC, acquisition.GradeD, true},
		{"D beats F", acquisition.GradeD, acquisition.GradeF, true},
		{"A beats F", acquisition.GradeA, acquisition.GradeF, true},
		{"an equal grade does not beat itself", acquisition.GradeC, acquisition.GradeC, false},
		{"B does not beat A", acquisition.GradeB, acquisition.GradeA, false},
		{"F does not beat D", acquisition.GradeF, acquisition.GradeD, false},
		{"an unknown candidate beats nothing", acquisition.Grade("Z"), acquisition.GradeF, false},
		{"nothing is beaten by an unknown current grade", acquisition.GradeA, acquisition.Grade(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.candidate.Beats(tc.current); got != tc.want {
				t.Errorf("%q.Beats(%q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
			}
		})
	}
}
