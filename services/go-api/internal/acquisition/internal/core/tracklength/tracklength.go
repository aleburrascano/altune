package tracklength

import (
	"altune/go-api/internal/acquisition/internal/core/domain"
	"altune/go-api/internal/acquisition/internal/core/ports"
)

type Policy struct {
	saved    float64
	resolved float64
}

func New(saved, resolved float64, resolvedDoubted bool) Policy {
	if resolvedDoubted {
		resolved = 0
	}
	return Policy{saved: saved, resolved: resolved}
}

func (p Policy) Resolved() float64 {
	return p.resolved
}

func (p Policy) Expected() float64 {
	if p.saved > 0 {
		return p.saved
	}
	return p.resolved
}

func (p Policy) Corroborated() bool {
	if p.saved <= 0 || p.resolved <= 0 {
		return false
	}
	return domain.LengthAuthoritative.Agrees(p.saved, p.resolved)
}

func (p Policy) Plausible(candidate ports.AudioCandidate) bool {
	if candidate.Duration <= 0 {
		return true
	}
	if candidate.IsResolved() {
		return p.Expected() <= 0 || p.accepts(candidate.Duration)
	}
	return p.saved <= 0 || p.accepts(candidate.Duration)
}

func (p Policy) CanJudge(candidate ports.AudioCandidate) bool {
	if candidate.IsResolved() && candidate.Duration > 0 {
		return true
	}
	return p.Expected() > 0
}

func (p Policy) Judge(candidate ports.AudioCandidate, actual float64) (acceptable bool, heldAgainst float64) {
	if candidate.IsResolved() && candidate.Duration > 0 {
		if !domain.LengthAuthoritative.Agrees(candidate.Duration, actual) {
			return false, candidate.Duration
		}
		return p.Expected() <= 0 || p.accepts(actual), p.Expected()
	}
	return p.accepts(actual), p.Expected()
}

func (p Policy) accepts(actual float64) bool {
	if p.Corroborated() {
		return domain.LengthAuthoritative.Agrees(p.saved, actual)
	}
	return (p.saved > 0 && domain.LengthLoose.Agrees(p.saved, actual)) ||
		(p.resolved > 0 && domain.LengthLoose.Agrees(p.resolved, actual))
}
