package service

import (
	"altune/go-api/internal/download/internal/domain"
	"altune/go-api/internal/shared/tracklength"
)

type LengthPolicy struct {
	saved    float64
	resolved float64
}

func NewLengthPolicy(saved, resolved float64) LengthPolicy {
	return LengthPolicy{saved: saved, resolved: resolved}
}

func DoubtedLengthPolicy(saved float64) LengthPolicy {
	return LengthPolicy{saved: saved}
}

func (p LengthPolicy) Resolved() float64 {
	return p.resolved
}

func (p LengthPolicy) Expected() float64 {
	if p.saved > 0 {
		return p.saved
	}
	return p.resolved
}

func (p LengthPolicy) Corroborated() bool {
	if p.saved <= 0 || p.resolved <= 0 {
		return false
	}
	return SameRecording(p.saved, p.resolved)
}

func (p LengthPolicy) Plausible(candidate domain.AudioCandidate) bool {
	if candidate.Duration <= 0 {
		return true
	}
	if candidate.IsResolved() {
		return p.Expected() <= 0 || p.accepts(candidate.Duration)
	}
	return p.saved <= 0 || p.accepts(candidate.Duration)
}

type FileJudge struct {
	policy    LengthPolicy
	candidate domain.AudioCandidate
}

func (p LengthPolicy) FileJudge(candidate domain.AudioCandidate) (FileJudge, bool) {
	if !ownLength(candidate) && p.Expected() <= 0 {
		return FileJudge{}, false
	}
	return FileJudge{policy: p, candidate: candidate}, true
}

func (j FileJudge) Judge(actual float64) (acceptable bool, heldAgainst float64) {
	p, candidate := j.policy, j.candidate
	if !ownLength(candidate) {
		return p.accepts(actual), p.Expected()
	}
	if !SameRecording(candidate.Duration, actual) {
		return false, candidate.Duration
	}
	return p.Expected() <= 0 || p.accepts(actual), p.Expected()
}

func ownLength(candidate domain.AudioCandidate) bool {
	return candidate.IsResolved() && candidate.Duration > 0
}

func (p LengthPolicy) accepts(actual float64) bool {
	if p.Corroborated() {
		return SameRecording(p.saved, actual)
	}
	return (p.saved > 0 && domain.LengthLoose.Agrees(p.saved, actual)) ||
		(p.resolved > 0 && domain.LengthLoose.Agrees(p.resolved, actual))
}

func SameRecording(expectedSeconds, actualSeconds float64) bool {
	expected, expectedErr := tracklength.TrackLengthOf(expectedSeconds)
	actual, actualErr := tracklength.TrackLengthOf(actualSeconds)
	return expectedErr == nil && actualErr == nil && expected.SameRecording(actual)
}
