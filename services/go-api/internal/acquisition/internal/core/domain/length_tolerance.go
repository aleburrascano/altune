package domain

import "math"

type LengthTolerance struct {
	SlackSeconds float64
	Fraction     float64
}

var (
	LengthTight = LengthTolerance{SlackSeconds: 2}
	LengthLoose = LengthTolerance{SlackSeconds: 15, Fraction: 0.07}
)

func (t LengthTolerance) Allowance(expectedSeconds float64) float64 {
	return math.Max(t.SlackSeconds, expectedSeconds*t.Fraction)
}

func (t LengthTolerance) Agrees(expectedSeconds, actualSeconds float64) bool {
	if expectedSeconds <= 0 || actualSeconds <= 0 {
		return false
	}
	return math.Abs(expectedSeconds-actualSeconds) <= t.Allowance(expectedSeconds)
}
