package testpoll_test

import (
	"altune/go-api/internal/shared/testpoll"
	"sync/atomic"
	"testing"
	"time"
)

func TestWithinReturnsAsSoonAsTheConditionHolds(t *testing.T) {
	var calls atomic.Int32
	got := testpoll.Within(5*time.Second, func() bool { return calls.Add(1) >= 3 })

	if !got {
		t.Fatal("Within = false, want true once the condition held")
	}
	if n := calls.Load(); n != 3 {
		t.Fatalf("condition checked %d times, want 3: it must stop polling when it holds", n)
	}
}

func TestWithinChecksOnceBeforeWaiting(t *testing.T) {
	start := time.Now()
	if !testpoll.Within(time.Hour, func() bool { return true }) {
		t.Fatal("Within = false for a condition that already holds")
	}
	if time.Since(start) > time.Second {
		t.Fatal("Within waited although the condition already held")
	}
}

func TestWithinGivesTheFinalWordToTheConditionAtTheDeadline(t *testing.T) {
	if testpoll.Within(30*time.Millisecond, func() bool { return false }) {
		t.Fatal("Within = true for a condition that never holds")
	}
}

type recorder struct {
	testing.TB
	failed  bool
	message string
}

func (r *recorder) Helper() {}

func (r *recorder) Fatalf(format string, args ...any) {
	r.failed = true
	r.message = format
}

func TestUntilFailsNamingWhatItWaitedFor(t *testing.T) {
	r := &recorder{TB: t}

	testpoll.Until(r, 20*time.Millisecond, "the queue row to be locked", func() bool { return false })

	if !r.failed {
		t.Fatal("Until did not fail for a condition that never holds")
	}
}

func TestUntilPassesWhenTheConditionHolds(t *testing.T) {
	testpoll.Until(t, time.Second, "an immediate condition", func() bool { return true })
}
