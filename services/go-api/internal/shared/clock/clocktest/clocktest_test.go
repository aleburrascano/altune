package clocktest_test

import (
	"altune/go-api/internal/shared/clock/clocktest"
	"sync"
	"testing"
	"time"
)

var start = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func TestManual_ReadsTheTimeItWasGiven(t *testing.T) {
	m := clocktest.NewManual(start)

	if got := m.Now(); !got.Equal(start) {
		t.Errorf("Now() = %v, want %v", got, start)
	}
}

func TestManual_AdvanceMovesTheTimeForwardByExactlyTheDuration(t *testing.T) {
	m := clocktest.NewManual(start)

	m.Advance(90 * time.Second)

	if got, want := m.Now(), start.Add(90*time.Second); !got.Equal(want) {
		t.Errorf("Now() = %v, want %v", got, want)
	}
}

func TestManual_SetReplacesTheTime(t *testing.T) {
	m := clocktest.NewManual(start)
	later := start.Add(24 * time.Hour)

	m.Set(later)

	if got := m.Now(); !got.Equal(later) {
		t.Errorf("Now() = %v, want %v", got, later)
	}
}

func TestManual_FuncFollowsLaterAdvances(t *testing.T) {
	m := clocktest.NewManual(start)
	read := m.Func()

	m.Advance(time.Minute)

	if got, want := read(), start.Add(time.Minute); !got.Equal(want) {
		t.Errorf("clock.Func() = %v, want %v", got, want)
	}
}

func TestManual_IsSafeToAdvanceAndReadFromManyGoroutines(t *testing.T) {
	m := clocktest.NewManual(start)
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(2)
		go func() { defer wg.Done(); m.Advance(time.Second) }()
		go func() { defer wg.Done(); m.Now() }()
	}
	wg.Wait()

	if got, want := m.Now(), start.Add(50*time.Second); !got.Equal(want) {
		t.Errorf("Now() = %v, want %v after 50 advances", got, want)
	}
}
