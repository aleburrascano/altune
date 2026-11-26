package backoff

import (
	"math/rand/v2"
	"time"
)

const defaultFactor = 2

type Policy struct {
	Base   time.Duration
	Cap    time.Duration
	Factor int
	Spread float64
}

func (p Policy) Ceiling(attempt int) time.Duration {
	factor := int64(p.Factor)
	if p.Factor < 1 {
		factor = defaultFactor
	}
	d := p.Base
	for i := 1; i < attempt && d < p.Cap; i++ {
		if int64(d) > int64(p.Cap)/factor {
			return p.Cap
		}
		d = time.Duration(int64(d) * factor)
	}
	return min(d, p.Cap)
}

func (p Policy) DelayAt(attempt int, unit float64) time.Duration {
	ceiling := p.Ceiling(attempt)
	if p.Spread > 0 {
		return ceiling + time.Duration((unit*2-1)*p.Spread*float64(ceiling))
	}
	half := ceiling / 2
	return half + time.Duration(unit*float64(ceiling-half))
}

func (p Policy) Delay(attempt int) time.Duration {
	return p.DelayAt(attempt, rand.Float64())
}
