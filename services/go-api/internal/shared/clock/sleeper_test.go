package clock_test

import (
	"altune/go-api/internal/shared/clock"
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"
)

func TestSleep_ReturnsNilAfterTheDuration(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()

		err := clock.Sleep(context.Background(), 3*time.Second)

		if err != nil || time.Since(start) != 3*time.Second {
			t.Fatalf("Sleep = %v after %v, want nil after exactly 3s", err, time.Since(start))
		}
	})
}

func TestSleep_EndsAtOnceWithTheContextError(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		time.AfterFunc(time.Second, cancel)
		start := time.Now()

		err := clock.Sleep(ctx, time.Hour)

		if !errors.Is(err, context.Canceled) || time.Since(start) != time.Second {
			t.Fatalf("Sleep = %v after %v, want Canceled after 1s", err, time.Since(start))
		}
	})
}

func TestSleep_ANonPositiveDurationDoesNotWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()

		err := clock.Sleep(context.Background(), 0)

		if err != nil || !time.Now().Equal(start) {
			t.Fatalf("Sleep(0) = %v after %v, want nil with no wait", err, time.Since(start))
		}
	})
}

func TestSleep_ANonPositiveDurationOnACanceledContextReportsIt(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if err := clock.Sleep(ctx, 0); !errors.Is(err, context.Canceled) {
		t.Fatalf("Sleep(0) on a canceled context = %v, want Canceled", err)
	}
}

func TestNewTimer_FiresAfterTheDurationAndStopPreventsIt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var newTimer clock.TimerFunc = clock.NewTimer
		start := time.Now()
		fires, _ := newTimer(2 * time.Second)
		stoppedFires, stop := newTimer(time.Hour)

		if !stop() {
			t.Fatal("stop() = false for a pending timer, want true")
		}
		got := <-fires

		if got.Sub(start) != 2*time.Second {
			t.Fatalf("fired after %v, want 2s", got.Sub(start))
		}
		select {
		case <-stoppedFires:
			t.Fatal("a stopped timer fired")
		default:
		}
	})
}

func TestSleep_SatisfiesSleeper(t *testing.T) {
	var sleep clock.Sleeper = clock.Sleep

	if err := sleep(context.Background(), 0); err != nil {
		t.Fatalf("Sleep through the Sleeper type = %v, want nil", err)
	}
}
