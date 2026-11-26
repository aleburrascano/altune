package audioquality_test

import (
	"altune/go-api/internal/download"
	"testing"
)

func TestBeats_AUserIsTheTopOfTheScale(t *testing.T) {
	if !download.GradeAUser.Beats(download.GradeA) || download.GradeA.Beats(download.GradeAUser) || download.GradeAUser.Beats(download.GradeAUser) {
		t.Error("A_user must beat A, and nothing beats A_user")
	}
	if !download.GradeAUser.IsKnown() || !download.GradeAUser.IsTop() || !download.GradeA.IsTop() || download.GradeB.IsTop() {
		t.Error("only A and A_user are top grades")
	}
}

func TestBeats(t *testing.T) {
	cases := []struct {
		name      string
		candidate download.Grade
		current   download.Grade
		want      bool
	}{
		{"A beats B", download.GradeA, download.GradeB, true},
		{"B beats C", download.GradeB, download.GradeC, true},
		{"C beats D", download.GradeC, download.GradeD, true},
		{"D beats F", download.GradeD, download.GradeF, true},
		{"A beats F", download.GradeA, download.GradeF, true},
		{"an equal grade does not beat itself", download.GradeC, download.GradeC, false},
		{"B does not beat A", download.GradeB, download.GradeA, false},
		{"F does not beat D", download.GradeF, download.GradeD, false},
		{"an unknown candidate beats nothing", download.Grade("Z"), download.GradeF, false},
		{"nothing is beaten by an unknown current grade", download.GradeA, download.Grade(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.candidate.Beats(tc.current); got != tc.want {
				t.Errorf("%q.Beats(%q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
			}
		})
	}
}
