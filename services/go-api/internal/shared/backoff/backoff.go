package backoff

import (
	"math/rand/v2"
	"time"
)

type Policy struct {
	Base time.Duration
	Cap  time.Duration
}

func (p Policy) Ceiling(attempt int) time.Duration {
	d := p.Base
	for i := 1; i < attempt && d < p.Cap; i++ {
		d *= 2
	}
	return min(d, p.Cap)
}

func (p Policy) DelayAt(attempt int, unit float64) time.Duration {
	ceiling := p.Ceiling(attempt)
	half := ceiling / 2
	return half + time.Duration(unit*float64(ceiling-half))
}

func (p Policy) Delay(attempt int) time.Duration {
	return p.DelayAt(attempt, rand.Float64())
}
