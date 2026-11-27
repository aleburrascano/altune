package backoff

import (
	"errors"
	"fmt"
	"math"
	"time"
)

const defaultFactor = 2

var (
	errBaseNotPositive = errors.New("backoff: base must be positive")
	errCapNotPositive  = errors.New("backoff: cap must be positive")
	errCapBelowBase    = errors.New("backoff: cap must not be below the base")
	errFactorBelowTwo  = errors.New("backoff: factor must be at least 2")
	errSpreadOutside   = errors.New("backoff: spread must be within [0, 1]")
)

type Policy struct {
	base   time.Duration
	limit  time.Duration
	factor int64
	spread float64
}

type option func(*Policy) error

func WithFactor(factor int) option {
	return func(p *Policy) error {
		if factor < defaultFactor {
			return fmt.Errorf("%w, got %d", errFactorBelowTwo, factor)
		}
		p.factor = int64(factor)
		return nil
	}
}

func WithSpread(spread float64) option {
	return func(p *Policy) error {
		if spread < 0 || spread > 1 || math.IsNaN(spread) {
			return fmt.Errorf("%w, got %v", errSpreadOutside, spread)
		}
		p.spread = spread
		return nil
	}
}

func capped(base, limit time.Duration, opts ...option) (Policy, error) {
	if limit <= 0 {
		return Policy{}, fmt.Errorf("%w, got %v", errCapNotPositive, limit)
	}
	if limit < base {
		return Policy{}, fmt.Errorf("%w: base %v, cap %v", errCapBelowBase, base, limit)
	}
	return build(base, limit, opts)
}

func uncapped(base time.Duration, opts ...option) (Policy, error) {
	return build(base, time.Duration(math.MaxInt64), opts)
}

func MustCapped(base, limit time.Duration, opts ...option) Policy {
	p, err := capped(base, limit, opts...)
	if err != nil {
		panic(err)
	}
	return p
}

func MustUncapped(base time.Duration, opts ...option) Policy {
	p, err := uncapped(base, opts...)
	if err != nil {
		panic(err)
	}
	return p
}

func build(base, limit time.Duration, opts []option) (Policy, error) {
	if base <= 0 {
		return Policy{}, fmt.Errorf("%w, got %v", errBaseNotPositive, base)
	}
	p := Policy{base: base, limit: limit, factor: defaultFactor}
	for _, opt := range opts {
		if err := opt(&p); err != nil {
			return Policy{}, err
		}
	}
	return p, nil
}

func (p Policy) Ceiling(attempt int) time.Duration {
	d := p.base
	for i := 1; i < attempt && d < p.limit; i++ {
		if int64(d) > int64(p.limit)/p.factor {
			return p.limit
		}
		d = time.Duration(int64(d) * p.factor)
	}
	return min(d, p.limit)
}

func (p Policy) DelayAt(attempt int, unit float64) time.Duration {
	ceiling := p.Ceiling(attempt)
	if p.spread > 0 {
		delta := time.Duration((unit*2 - 1) * p.spread * float64(ceiling))
		if delta > 0 && ceiling > time.Duration(math.MaxInt64)-delta {
			return time.Duration(math.MaxInt64)
		}
		return ceiling + delta
	}
	half := ceiling / 2
	return half + time.Duration(unit*float64(ceiling-half))
}
