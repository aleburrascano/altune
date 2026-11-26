package parallel_test

import (
	"altune/go-api/internal/shared/parallel"
	"context"
	"errors"
	"testing"
)

func TestSlots_TryAcquireShedsWhenAllSlotsAreHeld(t *testing.T) {
	s := parallel.MustNewSlots(2)

	first, second, third := s.TryAcquire(), s.TryAcquire(), s.TryAcquire()

	if !first || !second || third {
		t.Fatalf("TryAcquire on 2 slots = %v, %v, %v, want true, true, false", first, second, third)
	}
}

func TestSlots_ReleaseFreesASlotForTheNextCaller(t *testing.T) {
	s := parallel.MustNewSlots(1)
	s.TryAcquire()

	s.Release()

	if !s.TryAcquire() {
		t.Fatal("a released slot was not available")
	}
}

func TestSlots_AcquireWaitsForARelease(t *testing.T) {
	s := parallel.MustNewSlots(1)
	s.TryAcquire()
	got := make(chan error, 1)
	go func() { got <- s.Acquire(context.Background()) }()

	s.Release()

	if err := <-got; err != nil {
		t.Fatalf("Acquire after Release = %v, want nil", err)
	}
}

func TestSlots_AcquireGivesUpWhenTheContextEnds(t *testing.T) {
	s := parallel.MustNewSlots(1)
	s.TryAcquire()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := s.Acquire(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Acquire on a full pool with a cancelled context = %v, want context.Canceled", err)
	}
}

func TestNewSlots_RefusesALimitBelowOne(t *testing.T) {
	for _, n := range []int{0, -1} {
		if _, err := parallel.NewSlots(n); !errors.Is(err, parallel.ErrSlotsBelowOne) {
			t.Errorf("NewSlots(%d) error = %v, want ErrSlotsBelowOne", n, err)
		}
	}
}
