package grade

import "testing"

func TestBeats_AUserIsTheTopOfTheScale(t *testing.T) {
	if !AUser.Beats(A) || A.Beats(AUser) || AUser.Beats(AUser) {
		t.Error("A_user must beat A, and nothing beats A_user")
	}
	if !AUser.IsKnown() || !AUser.IsTop() || !A.IsTop() || B.IsTop() {
		t.Error("only A and A_user are top grades")
	}
}

func TestBeats(t *testing.T) {
	cases := []struct {
		name      string
		candidate Grade
		current   Grade
		want      bool
	}{
		{"A beats B", A, B, true},
		{"B beats C", B, C, true},
		{"C beats D", C, D, true},
		{"D beats F", D, F, true},
		{"A beats F", A, F, true},
		{"an equal grade does not beat itself", C, C, false},
		{"B does not beat A", B, A, false},
		{"F does not beat D", F, D, false},
		{"an unknown candidate beats nothing", Grade("Z"), F, false},
		{"nothing is beaten by an unknown current grade", A, Grade(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.candidate.Beats(tc.current); got != tc.want {
				t.Errorf("%q.Beats(%q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
			}
		})
	}
}
