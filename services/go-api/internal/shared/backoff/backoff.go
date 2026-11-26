package backoff

import (
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"
)

const defaultFactor = 2

var (
	ErrBaseNotPositive = errors.New("backoff: base must be positive")
	ErrCapNotPositive  = errors.New("backoff: cap must be positive")
	ErrCapBelowBase    = errors.New("backoff: cap must not be below the base")
	ErrFactorBelowTwo  = errors.New("backoff: factor must be at least 2")
	ErrSpreadOutside   = errors.New("backoff: spread must be within [0, 1]")
)

type Policy struct {
	base   time.Duration
	limit  time.Duration
	factor int64
	spread float64
}

type Option func(*Policy) error

func WithFactor(factor int) Option {
	return func(p *Policy) error {
		if factor < defaultFactor {
			return fmt.Errorf("%w, got %d", ErrFactorBelowTwo, factor)
		}
		p.factor = int64(factor)
		return nil
	}
}

func WithSpread(spread float64) Option {
	return func(p *Policy) error {
		if spread < 0 || spread > 1 || math.IsNaN(spread) {
			return fmt.Errorf("%w, got %v", ErrSpreadOutside, spread)
		}
		p.spread = spread
		return nil
	}
}

func Capped(base, limit time.Duration, opts ...Option) (Policy, error) {
	if limit <= 0 {
		return Policy{}, fmt.Errorf("%w, got %v", ErrCapNotPositive, limit)
	}
	if limit < base {
		return Policy{}, fmt.Errorf("%w: base %v, cap %v", ErrCapBelowBase, base, limit)
	}
	return build(base, limit, opts)
}

func Uncapped(base time.Duration, opts ...Option) (Policy, error) {
	return build(base, time.Duration(math.MaxInt64), opts)
}

func MustCapped(base, limit time.Duration, opts ...Option) Policy {
	p, err := Capped(base, limit, opts...)
	if err != nil {
		panic(err)
	}
	return p
}

func MustUncapped(base time.Duration, opts ...Option) Policy {
	p, err := Uncapped(base, opts...)
	if err != nil {
		panic(err)
	}
	return p
}

func build(base, limit time.Duration, opts []Option) (Policy, error) {
	if base <= 0 {
		return Policy{}, fmt.Errorf("%w, got %v", ErrBaseNotPositive, base)
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
		return ceiling + time.Duration((unit*2-1)*p.spread*float64(ceiling))
	}
	half := ceiling / 2
	return half + time.Duration(unit*float64(ceiling-half))
}

func (p Policy) Delay(attempt int) time.Duration {
	return p.DelayAt(attempt, rand.Float64())
}
