package audiojudge

const (
	defaultProvenMax      = 0.10
	defaultLikelyMax      = 0.18
	defaultAbsentMin      = 0.35
	defaultMinOverlapSecs = 20.0
)

type Cuts struct {
	ProvenMax      float64
	LikelyMax      float64
	AbsentMin      float64
	MinOverlapSecs float64
}

func DefaultCuts() Cuts {
	return Cuts{ProvenMax: defaultProvenMax, LikelyMax: defaultLikelyMax, AbsentMin: defaultAbsentMin, MinOverlapSecs: defaultMinOverlapSecs}
}
