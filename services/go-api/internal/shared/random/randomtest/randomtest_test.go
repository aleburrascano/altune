package randomtest_test

import (
	"altune/go-api/internal/shared/random/randomtest"
	"sync"
	"testing"
)

func TestSequence_ReturnsEachValueInOrderThenRepeatsTheLast(t *testing.T) {
	unit := randomtest.Sequence(0.1, 0.5, 0.9)

	got := []float64{unit(), unit(), unit(), unit(), unit()}

	want := []float64{0.1, 0.5, 0.9, 0.9, 0.9}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("draws = %v, want %v", got, want)
		}
	}
}

func TestSequence_IsNotChangedByTheCallersLaterEditsToTheSlice(t *testing.T) {
	values := []float64{0.1, 0.2}
	unit := randomtest.Sequence(values...)

	values[0] = 5

	if got := unit(); got != 0.1 {
		t.Fatalf("first draw = %v, want 0.1 as given", got)
	}
}

func TestFixed_ReturnsTheSameValueEveryTime(t *testing.T) {
	unit := randomtest.Fixed(0.25)

	for range 5 {
		if got := unit(); got != 0.25 {
			t.Fatalf("Fixed(0.25) drew %v", got)
		}
	}
}

func TestSequence_RefusesNoValuesAndValuesOutsideTheUnitInterval(t *testing.T) {
	cases := map[string][]float64{
		"no values":    {},
		"negative":     {-0.1},
		"exactly one":  {1},
		"above one":    {0.2, 1.5},
		"not a number": {nan()},
	}
	for name, values := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatalf("Sequence(%v) was accepted, want a panic", values)
				}
			}()

			randomtest.Sequence(values...)
		})
	}
}

func nan() float64 {
	zero := 0.0
	return zero / zero
}

func TestSequence_IsSafeFromManyGoroutines(t *testing.T) {
	unit := randomtest.Sequence(0.1, 0.2)
	var wg sync.WaitGroup

	for range 50 {
		wg.Go(func() { unit() })
	}
	wg.Wait()
}
