package clock_test

import (
	"altune/go-api/internal/shared/clock"
	"testing"
	"time"
)

func TestFunc_UTCReadsTheClockInUTC(t *testing.T) {
	zone := time.FixedZone("UTC+5", 5*3600)
	instant := time.Date(2026, 11, 25, 14, 0, 0, 0, zone)
	now := clock.Func(func() time.Time { return instant })

	got := now.UTC()

	if got.Location() != time.UTC || !got.Equal(instant) {
		t.Errorf("UTC() = %v, want the same instant in UTC", got)
	}
}

func TestNow_IsAFuncThatReadsTheWallClock(t *testing.T) {
	var now clock.Func = clock.Now

	before := time.Now()
	got := now()

	if got.Before(before) || time.Since(got) < 0 {
		t.Errorf("Now() = %v, want a reading taken after %v", got, before)
	}
}

func TestFunc_AcceptsTimeNow(t *testing.T) {
	var now clock.Func = time.Now

	if now().IsZero() {
		t.Error("the wall clock read as the zero time")
	}
}
