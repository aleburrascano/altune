package service

import (
	"altune/go-api/internal/acquisition/domain"
	"altune/go-api/internal/acquisition/ports"
)

func durationWithinTolerance(expected, actual float64) bool {
	return domain.LengthLoose.Agrees(expected, actual)
}

func durationWithinAuthoritativeTolerance(expected, actual float64) bool {
	return domain.LengthAuthoritative.Agrees(expected, actual)
}

func (ac *AcquisitionContext) resolvedLength() float64 {
	if ac.Identity.ReferenceDoubted {
		return 0
	}
	return ac.Identity.Duration
}

func (ac *AcquisitionContext) ExpectedLength() float64 {
	switch {
	case ac.Track.CatalogDuration > 0:
		return ac.Track.CatalogDuration
	default:
		return ac.resolvedLength()
	}
}

func (ac *AcquisitionContext) lengthCorroborated() bool {
	saved, resolved := ac.Track.CatalogDuration, ac.resolvedLength()
	if saved <= 0 || resolved <= 0 {
		return false
	}
	return durationWithinAuthoritativeTolerance(saved, resolved)
}

func (ac *AcquisitionContext) candidateDurationPlausible(candidate ports.AudioCandidate) bool {
	if candidate.Duration <= 0 {
		return true
	}
	if candidate.IsResolved() {
		return ac.ExpectedLength() <= 0 || ac.durationAcceptable(candidate.Duration)
	}
	return ac.Track.CatalogDuration <= 0 || ac.durationAcceptable(candidate.Duration)
}

func expectedLength(ac *AcquisitionContext, candidate ports.AudioCandidate) float64 {
	if candidate.IsResolved() && candidate.Duration > 0 {
		return candidate.Duration
	}
	return ac.ExpectedLength()
}

func lengthHeldAgainst(ac *AcquisitionContext, candidate ports.AudioCandidate, actual float64) float64 {
	if candidate.IsResolved() && candidate.Duration > 0 && !durationWithinAuthoritativeTolerance(candidate.Duration, actual) {
		return candidate.Duration
	}
	return ac.ExpectedLength()
}

func (ac *AcquisitionContext) fileLengthAcceptable(candidate ports.AudioCandidate, actual float64) bool {
	if candidate.IsResolved() && candidate.Duration > 0 {
		return durationWithinAuthoritativeTolerance(candidate.Duration, actual) &&
			(ac.ExpectedLength() <= 0 || ac.durationAcceptable(actual))
	}
	return ac.durationAcceptable(actual)
}

func (ac *AcquisitionContext) durationAcceptable(actual float64) bool {
	saved, resolved := ac.Track.CatalogDuration, ac.resolvedLength()
	switch {
	case ac.lengthCorroborated():
		return durationWithinAuthoritativeTolerance(saved, actual)
	case saved <= 0 && resolved > 0:
		return durationWithinTolerance(resolved, actual)
	case resolved > 0:
		return durationWithinTolerance(saved, actual) || durationWithinTolerance(resolved, actual)
	default:
		return durationWithinTolerance(saved, actual)
	}
}
