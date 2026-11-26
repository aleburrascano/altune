package clocktest_test

import (
	"altune/go-api/internal/shared/clock/clocktest"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
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

func TestManualSleeper_WorksOnAZeroValueManual(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var m clocktest.Manual
		done := sleepInBackground(context.Background(), &m, time.Second)
		synctest.Wait()

		m.Advance(time.Second)

		if err := <-done; err != nil {
			t.Fatalf("sleep on a zero-value Manual = %v, want nil", err)
		}
	})
}

func TestManualTimer_FiresWhenTheClockReachesTheDeadlineAndStopCancelsIt(t *testing.T) {
	m := clocktest.NewManual(start)
	fires, _ := m.Timer()(time.Minute)
	stoppedFires, stop := m.Timer()(time.Minute)

	m.Advance(59 * time.Second)
	select {
	case <-fires:
		t.Fatal("the timer fired a second early")
	default:
	}
	if !stop() {
		t.Fatal("stop() = false for a pending timer, want true")
	}
	m.Advance(time.Second)

	select {
	case got := <-fires:
		if !got.Equal(start.Add(time.Minute)) {
			t.Fatalf("fired with %v, want the clock's time %v", got, start.Add(time.Minute))
		}
	default:
		t.Fatal("the timer did not fire at its deadline")
	}
	select {
	case <-stoppedFires:
		t.Fatal("a stopped timer fired")
	default:
	}
	if stop() {
		t.Fatal("stop() = true for a timer already stopped, want false")
	}
}

func TestManualTicker_KeepsAFixedRateAndStopEndsIt(t *testing.T) {
	m := clocktest.NewManual(start)
	ticks, stop := m.Ticker()(10 * time.Second)

	m.Advance(9 * time.Second)
	select {
	case <-ticks:
		t.Fatal("ticked before the first interval")
	default:
	}

	m.Advance(5 * time.Second)
	<-ticks

	m.Advance(6 * time.Second)
	select {
	case <-ticks:
	default:
		t.Fatal("no tick at 20s, a slow reader must not shift the schedule")
	}

	stop()
	m.Advance(time.Hour)
	select {
	case <-ticks:
		t.Fatal("a stopped ticker ticked")
	default:
	}
	if got := m.Sleeping(); got != 0 {
		t.Fatalf("Sleeping() = %d after stop, want 0", got)
	}
}

func TestManualTimer_ANonPositiveDurationFiresAtOnce(t *testing.T) {
	m := clocktest.NewManual(start)

	fires, stop := m.Timer()(0)

	select {
	case <-fires:
	default:
		t.Fatal("a zero-duration timer did not fire at once")
	}
	if stop() {
		t.Fatal("stop() = true for a fired timer, want false")
	}
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

func TestManualBlockUntil_ReturnsOnceEnoughSleepsTimersAndTickersAreWaiting(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := clocktest.NewManual(start)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		returned := make(chan struct{})
		go func() {
			m.BlockUntil(t, 3)
			close(returned)
		}()

		sleepInBackground(ctx, m, time.Minute)
		m.Timer()(time.Minute)
		synctest.Wait()
		select {
		case <-returned:
			t.Fatal("BlockUntil(3) returned with two waiters")
		default:
		}

		m.Ticker()(time.Minute)
		synctest.Wait()

		select {
		case <-returned:
		default:
			t.Fatal("BlockUntil(3) did not return with three waiters")
		}
	})
}

func TestManualBlockUntil_ReturnsAtOnceWhenEnoughAreAlreadyWaitingAndWorksOnAZeroValue(t *testing.T) {
	var m clocktest.Manual
	m.Ticker()(time.Second)

	m.BlockUntil(t, 1)
	m.BlockUntil(t, 0)
}

type recordingTB struct {
	testing.TB
	failure string
}

func (r *recordingTB) Helper() {}

func (r *recordingTB) Fatalf(format string, args ...any) {
	r.failure = fmt.Sprintf(format, args...)
	runtime.Goexit()
}

func TestManualBlockUntil_FailsTheTestWhenTheWaitersNeverArrive(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := clocktest.NewManual(start)
		m.Ticker()(time.Minute)
		rec := &recordingTB{TB: t}
		done := make(chan struct{})

		go func() {
			defer close(done)
			m.BlockUntil(rec, 2)
		}()
		<-done

		if !strings.Contains(rec.failure, "BlockUntil(2) saw 1 waiting") {
			t.Fatalf("failure = %q, want BlockUntil to fail naming the 2 wanted and the 1 seen", rec.failure)
		}
	})
}
