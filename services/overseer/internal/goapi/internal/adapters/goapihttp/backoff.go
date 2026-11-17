package goapihttp

import "time"

const (
	defaultBackoffBase = 500 * time.Millisecond
	defaultBackoffMax  = 30 * time.Second
)

type Backoff interface {
	Backoff(attempt int) time.Duration
}

type expBackoff struct {
	base time.Duration
	max  time.Duration
}

func newExpBackoff(base, maximum time.Duration) expBackoff {
	if base <= 0 {
		base = defaultBackoffBase
	}
	if maximum < base {
		maximum = base
	}
	return expBackoff{base: base, max: maximum}
}

func (b expBackoff) Backoff(attempt int) time.Duration {
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
