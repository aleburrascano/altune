package randomtest

import (
	"altune/go-api/internal/shared/random"
	"fmt"
	"sync"
)

func Sequence(values ...float64) random.Unit {
	if len(values) == 0 {
		panic("randomtest: Sequence needs at least one value")
	}
	for _, v := range values {
		if !(v >= 0 && v < 1) {
			panic(fmt.Sprintf("randomtest: %v is outside [0, 1)", v))
		}
	}
	var mu sync.Mutex
	next := 0
	return func() float64 {
		mu.Lock()
		defer mu.Unlock()
		v := values[min(next, len(values)-1)]
		next++
		return v
	}
}

func Fixed(value float64) random.Unit {
	return Sequence(value)
}
