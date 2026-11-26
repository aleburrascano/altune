package random

import "math/rand/v2"

type Unit func() float64

func Real() float64 {
	return rand.Float64()
}
