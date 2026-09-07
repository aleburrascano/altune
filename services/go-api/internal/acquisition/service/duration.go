package service

import "math"

const (
	durationMatchSlackSecs = 15.0
	durationMatchFraction  = 0.07

	authoritativeSlackSecs = 5.0
	authoritativeFraction  = 0.03
)

func durationWithinTolerance(expected, actual float64) bool {
	tolerance := math.Max(durationMatchSlackSecs, expected*durationMatchFraction)
	return math.Abs(expected-actual) <= tolerance
}

func durationWithinAuthoritativeTolerance(expected, actual float64) bool {
	tolerance := math.Max(authoritativeSlackSecs, expected*authoritativeFraction)
	return math.Abs(expected-actual) <= tolerance
}

func (ac *AcquisitionContext) lengthCorroborated() bool {
	saved, resolved := ac.Track.Duration, ac.Identity.Duration
	if resolved <= 0 {
		return false
	}
	if saved <= 0 {
		return true
	}
	return durationWithinAuthoritativeTolerance(saved, resolved)
}

func (ac *AcquisitionContext) durationAcceptable(actual float64) bool {
	saved, resolved := ac.Track.Duration, ac.Identity.Duration
	if ac.lengthCorroborated() {
		if saved <= 0 {
			return durationWithinAuthoritativeTolerance(resolved, actual)
		}
		return durationWithinAuthoritativeTolerance(saved, actual)
	}
	if resolved > 0 {
		return durationWithinTolerance(saved, actual) || durationWithinTolerance(resolved, actual)
	}
	return durationWithinTolerance(saved, actual)
}
