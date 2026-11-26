package clocktest_test

import (
	"altune/go-api/internal/shared/clock/clocktest"
	"context"
	"errors"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func sleepInBackground(ctx context.Context, m *clocktest.Manual, d time.Duration) <-chan error {
	done := make(chan error, 1)
	sleep := m.Sleeper()
	go func() { done <- sleep(ctx, d) }()
	return done
}

func TestManualSleeper_WakesOnlyWhenTheClockReachesTheDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := clocktest.NewManual(start)
		done := sleepInBackground(context.Background(), m, time.Minute)
		synctest.Wait()

		m.Advance(59 * time.Second)
		synctest.Wait()
		select {
		case err := <-done:
			t.Fatalf("slept %v early with err %v", 59*time.Second, err)
		default:
		}
		if got := m.Sleeping(); got != 1 {
			t.Fatalf("Sleeping() = %d, want 1 before the deadline", got)
		}

		m.Advance(time.Second)
		if err := <-done; err != nil {
			t.Fatalf("sleep = %v, want nil at the deadline", err)
		}
		if got := m.Sleeping(); got != 0 {
			t.Fatalf("Sleeping() = %d, want 0 after waking", got)
		}
	})
}

func TestManualSleeper_SetPastTheDeadlineWakesTheSleeper(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := clocktest.NewManual(start)
		done := sleepInBackground(context.Background(), m, time.Hour)
		synctest.Wait()

		m.Set(start.Add(24 * time.Hour))

		if err := <-done; err != nil {
			t.Fatalf("sleep = %v, want nil", err)
		}
	})
}

func TestManualSleeper_ACanceledContextEndsTheSleepAndLeavesNoWaiter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := clocktest.NewManual(start)
		ctx, cancel := context.WithCancel(context.Background())
		done := sleepInBackground(ctx, m, time.Hour)
		synctest.Wait()

		cancel()

		if err := <-done; !errors.Is(err, context.Canceled) {
			t.Fatalf("sleep = %v, want Canceled", err)
		}
		if got := m.Sleeping(); got != 0 {
			t.Fatalf("Sleeping() = %d, want 0 after cancel", got)
		}
	})
}

func TestManualSleeper_ANonPositiveDurationReturnsAtOnce(t *testing.T) {
	m := clocktest.NewManual(start)

	if err := m.Sleeper()(context.Background(), 0); err != nil {
		t.Fatalf("sleep(0) = %v, want nil", err)
	}
	if got := m.Sleeping(); got != 0 {
		t.Fatalf("Sleeping() = %d, want 0", got)
	}
}

func TestManualSleeper_ManySleepersWakeInTheirOwnTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := clocktest.NewManual(start)
		short := sleepInBackground(context.Background(), m, time.Second)
		long := sleepInBackground(context.Background(), m, time.Hour)
		synctest.Wait()

		m.Advance(time.Minute)

		if err := <-short; err != nil {
			t.Fatalf("short sleep = %v, want nil", err)
		}
		if got := m.Sleeping(); got != 1 {
			t.Fatalf("Sleeping() = %d, want only the long sleeper left", got)
		}
		m.Advance(time.Hour)
		if err := <-long; err != nil {
			t.Fatalf("long sleep = %v, want nil", err)
		}
	})
}

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
