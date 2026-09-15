package goapi

import "time"

// defaultBackoffBase and defaultBackoffMax bound reconnect timing when the
// caller supplies no Backoff. go-api restarts (blue-green deploys) are seconds,
// so a sub-second first retry recovers fast while the cap keeps a persistently
// dead upstream from being hammered.
const (
	defaultBackoffBase = 500 * time.Millisecond
	defaultBackoffMax  = 30 * time.Second
)

// Backoff yields how long the consumer waits before each reconnect attempt. It
// is the seam that makes reconnect timing injectable: production uses capped
// exponential growth, while a test injects a tiny fixed sequence so it neither
// sleeps long nor flakes. attempt counts from 1 for the first retry after a drop.
type Backoff interface {
	// Backoff returns the wait before reconnect attempt n (n is clamped to >= 1).
	Backoff(attempt int) time.Duration
}

// ExpBackoff is capped exponential backoff: base, 2·base, 4·base, … up to max.
// Construct it with NewExpBackoff so the bounds are always sane.
type ExpBackoff struct {
	base time.Duration
	max  time.Duration
}

// NewExpBackoff builds a capped exponential backoff. A non-positive base falls
// back to the default, and a max below base is raised to base, so the returned
// policy always produces non-decreasing, bounded, positive waits.
func NewExpBackoff(base, maximum time.Duration) ExpBackoff {
	if base <= 0 {
		base = defaultBackoffBase
	}
	if maximum < base {
		maximum = base
	}
	return ExpBackoff{base: base, max: maximum}
}

// Backoff doubles from base per attempt, capped at max. It never overflows: once
// the next double would reach the cap it returns max directly.
func (b ExpBackoff) Backoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	d := b.base
	for i := 1; i < attempt; i++ {
		if d >= b.max/2 {
			return b.max
		}
		d *= 2
	}
	return d
}
