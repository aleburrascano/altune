package service

import (
	"altune/go-api/internal/eventstream/domain"
	"errors"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
)

func TestConnLimiter(t *testing.T) {
	l := NewConnLimiter(2, 0)
	if err := l.Acquire("u"); err != nil {
		t.Fatalf("first acquire should succeed: %v", err)
	}
	if err := l.Acquire("u"); err != nil {
		t.Fatalf("second acquire should succeed: %v", err)
	}
	if err := l.Acquire("u"); !errors.Is(err, domain.ErrUserConnLimit) {
		t.Fatalf("third acquire = %v, want domain.ErrUserConnLimit at the cap", err)
	}
	l.Release("u")
	if err := l.Acquire("u"); err != nil {
		t.Fatalf("acquire should succeed after a release frees a slot: %v", err)
	}
	if err := l.Acquire("v"); err != nil {
		t.Fatalf("a different user must have an independent budget: %v", err)
	}
}

func TestConnLimiter_GlobalCapAcrossUsers(t *testing.T) {
	l := NewConnLimiter(1, 2)
	if err := l.Acquire("a"); err != nil {
		t.Fatalf("a: %v", err)
	}
	if err := l.Acquire("a"); !errors.Is(err, domain.ErrUserConnLimit) {
		t.Fatalf("a again = %v, want domain.ErrUserConnLimit", err)
	}
	if err := l.Acquire("b"); err != nil {
		t.Fatalf("b must fit: the per-user rejection must not hold a global slot: %v", err)
	}
	if err := l.Acquire("c"); !errors.Is(err, domain.ErrGlobalConnLimit) {
		t.Fatalf("c = %v, want domain.ErrGlobalConnLimit", err)
	}
	l.Release("never-acquired")
	if err := l.Acquire("c"); !errors.Is(err, domain.ErrGlobalConnLimit) {
		t.Fatalf("c after stray release = %v, want domain.ErrGlobalConnLimit", err)
	}
	l.Release("a")
	if err := l.Acquire("c"); err != nil {
		t.Fatalf("c after a real release should fit: %v", err)
	}
}

func TestConnLimiter_GlobalCapHoldsUnderConcurrency(t *testing.T) {
	const limit, attempts = 5, 200
	l := NewConnLimiter(8, limit)
	var granted atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < attempts; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if l.Acquire(uuid.NewString()) == nil {
				granted.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := granted.Load(); got != limit {
		t.Fatalf("granted %d slots, want exactly %d", got, limit)
	}
}

func TestConnLimiter_ReleaseOfUserWithNoSlotsLeavesTotalUnchanged(t *testing.T) {
	l := NewConnLimiter(2, 1)
	if err := l.Acquire("a"); err != nil {
		t.Fatalf("a: %v", err)
	}
	l.Release("ghost")
	if err := l.Acquire("b"); !errors.Is(err, domain.ErrGlobalConnLimit) {
		t.Fatalf("b = %v, want ErrGlobalConnLimit: a stray release must not free a global slot", err)
	}
}

func TestConnLimiter_AcquireThenReleaseLeavesBothCountsZero(t *testing.T) {
	l := NewConnLimiter(1, 1)
	if err := l.Acquire("a"); err != nil {
		t.Fatalf("acquire: %v", err)
	}
	l.Release("a")
	if l.total != 0 || len(l.count) != 0 {
		t.Fatalf("total=%d count=%v, want both empty", l.total, l.count)
	}
}
