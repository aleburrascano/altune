package goapi

import "time"

const (
	defaultBackoffBase = 500 * time.Millisecond
	defaultBackoffMax  = 30 * time.Second
)

type Backoff interface {
	Backoff(attempt int) time.Duration
}

type ExpBackoff struct {
	base time.Duration
	max  time.Duration
}

func NewExpBackoff(base, maximum time.Duration) ExpBackoff {
	if base <= 0 {
		base = defaultBackoffBase
	}
	if maximum < base {
		maximum = base
	}
	return ExpBackoff{base: base, max: maximum}
}

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
