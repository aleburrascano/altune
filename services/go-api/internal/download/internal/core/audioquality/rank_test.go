package audioquality_test

import (
	"altune/go-api/internal/download/internal/core/audioquality"
	"testing"
)

func TestBeats_AUserIsTheTopOfTheScale(t *testing.T) {
	if !audioquality.AUser.Beats(audioquality.A) || audioquality.A.Beats(audioquality.AUser) || audioquality.AUser.Beats(audioquality.AUser) {
		t.Error("A_user must beat A, and nothing beats A_user")
	}
	if !audioquality.AUser.IsKnown() || !audioquality.AUser.IsTop() || !audioquality.A.IsTop() || audioquality.B.IsTop() {
		t.Error("only A and A_user are top grades")
	}
}

func TestBeats(t *testing.T) {
	cases := []struct {
		name      string
		candidate audioquality.Grade
		current   audioquality.Grade
		want      bool
	}{
		{"A beats B", audioquality.A, audioquality.B, true},
		{"B beats C", audioquality.B, audioquality.C, true},
		{"C beats D", audioquality.C, audioquality.D, true},
		{"D beats F", audioquality.D, audioquality.F, true},
		{"A beats F", audioquality.A, audioquality.F, true},
		{"an equal grade does not beat itself", audioquality.C, audioquality.C, false},
		{"B does not beat A", audioquality.B, audioquality.A, false},
		{"F does not beat D", audioquality.F, audioquality.D, false},
		{"an unknown candidate beats nothing", audioquality.Grade("Z"), audioquality.F, false},
		{"nothing is beaten by an unknown current grade", audioquality.A, audioquality.Grade(""), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.candidate.Beats(tc.current); got != tc.want {
				t.Errorf("%q.Beats(%q) = %v, want %v", tc.candidate, tc.current, got, tc.want)
			}
		})
	}
}
