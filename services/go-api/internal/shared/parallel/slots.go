package parallel

import (
	"context"
	"errors"
	"fmt"
)

var errSlotsBelowOne = errors.New("a slot pool needs at least one slot")

type Slots struct {
	free chan struct{}
}

func newSlots(n int) (*Slots, error) {
	if n < 1 {
		return nil, fmt.Errorf("%w: got %d", errSlotsBelowOne, n)
	}
	return &Slots{free: make(chan struct{}, n)}, nil
}

func MustNewSlots(n int) *Slots {
	s, err := newSlots(n)
	if err != nil {
		panic(err)
	}
	return s
}

func (s *Slots) Acquire(ctx context.Context) error {
	select {
	case s.free <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Slots) TryAcquire() bool {
	select {
	case s.free <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *Slots) Release() {
	<-s.free
}
