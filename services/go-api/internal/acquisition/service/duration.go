package service

import (
	"altune/go-api/internal/acquisition/ports"
	"math"
)

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

func (ac *AcquisitionContext) resolvedLength() float64 {
	if ac.Identity.ReferenceDoubted {
		return 0
	}
	return ac.Identity.Duration
}

func (ac *AcquisitionContext) savedLength() float64 {
	if ac.Track.CatalogDuration > 0 {
		return ac.Track.CatalogDuration
	}
	return ac.Track.Duration
}

func (ac *AcquisitionContext) ExpectedLength() float64 {
	switch {
	case ac.Track.CatalogDuration > 0:
		return ac.Track.CatalogDuration
	case ac.resolvedLength() > 0:
		return ac.resolvedLength()
	default:
		return ac.savedLength()
	}
}

func (ac *AcquisitionContext) lengthCorroborated() bool {
	saved, resolved := ac.savedLength(), ac.resolvedLength()
	if resolved <= 0 {
		return false
	}
	if saved <= 0 {
		return true
	}
	return durationWithinAuthoritativeTolerance(saved, resolved)
}

func (ac *AcquisitionContext) candidateDurationPlausible(candidate ports.AudioCandidate) bool {
	if candidate.Resolved || candidate.Duration <= 0 || ac.savedLength() <= 0 {
		return true
	}
	return ac.durationAcceptable(candidate.Duration)
}

func expectedLength(ac *AcquisitionContext, candidate ports.AudioCandidate) float64 {
	if candidate.Resolved && candidate.Duration > 0 {
		return candidate.Duration
	}
	return ac.ExpectedLength()
}

func (ac *AcquisitionContext) fileLengthAcceptable(candidate ports.AudioCandidate, actual float64) bool {
	if candidate.Resolved && candidate.Duration > 0 {
		return durationWithinAuthoritativeTolerance(candidate.Duration, actual)
	}
	return ac.durationAcceptable(actual)
}

func (ac *AcquisitionContext) durationAcceptable(actual float64) bool {
	saved, resolved := ac.savedLength(), ac.resolvedLength()
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
