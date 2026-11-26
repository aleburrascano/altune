package random_test

import (
	"altune/go-api/internal/shared/random"
	"testing"
)

func TestReal_StaysInTheHalfOpenUnitInterval(t *testing.T) {
	var unit random.Unit = random.Real

	for range 10_000 {
		if got := unit(); got < 0 || got >= 1 {
			t.Fatalf("Real() = %v, want a value in [0, 1)", got)
		}
	}
}

func TestReal_IsNotConstant(t *testing.T) {
	first := random.Real()

	for range 100 {
		if random.Real() != first {
			return
		}
	}
	t.Fatal("Real() returned the same value 101 times")
}
