package domain

import "math"

type LengthMargin struct {
	SlackSeconds float64
	Fraction     float64
}

var (
	LengthTight = LengthMargin{SlackSeconds: 2}
	LengthLoose = LengthMargin{SlackSeconds: 15, Fraction: 0.07}
)

func (t LengthMargin) Allowance(expectedSeconds float64) float64 {
	return math.Max(t.SlackSeconds, expectedSeconds*t.Fraction)
}

func (t LengthMargin) Agrees(expectedSeconds, actualSeconds float64) bool {
	if expectedSeconds <= 0 || actualSeconds <= 0 {
		return false
	}
	return math.Abs(expectedSeconds-actualSeconds) <= t.Allowance(expectedSeconds)
}
