package domain

import "slices"

const Ungraded = GradeD

var worstToBest = []Grade{GradeF, GradeD, GradeC, GradeB, GradeA, GradeAUser}

func (g Grade) rank() int {
	for i, known := range worstToBest {
		if known == g {
			return i
		}
	}
	return -1
}

func (g Grade) IsKnown() bool { return g.rank() >= 0 }

func (g Grade) Beats(other Grade) bool {
	return g.IsKnown() && other.IsKnown() && g.rank() > other.rank()
}

func (g Grade) IsTop() bool { return g == GradeA || g == GradeAUser }

func (g Grade) IsBestAvailable() bool { return g == BestAvailable }

func WorstFirst() []Grade { return slices.Clone(worstToBest) }
