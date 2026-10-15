package grade

const Ungraded = D

var worstToBest = []Grade{F, D, C, B, A, AUser}

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

func (g Grade) IsTop() bool { return g == A || g == AUser }
