package service

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"testing"
	"time"
)

// GitHub's documented secondary rate limits for content-generating requests
// such as creating an issue:
// https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api#about-secondary-rate-limits
// "no more than 80 content-generating requests per minute and no more than 500
// content-generating requests per hour".
const (
	githubContentPerMinute = 80
	githubContentPerHour   = 500
)

// TestDefaultSubmissionLimits_StayUnderGitHubContentLimits replays a worst-case
// flood (a fresh user every 500ms for two hours, so no per-user cap binds)
// against the default limits and asserts that no sliding minute or hour of
// admitted submissions exceeds GitHub's documented content-creation limits.
// Before #1116 the 30/minute cap alone admitted 1800 issues an hour.
func TestDefaultSubmissionLimits_StayUnderGitHubContentLimits(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	admission := newSubmissionAdmission(DefaultSubmissionLimits, clock.now)

	var admitted []time.Time
	for i := 0; i < 2*60*60*2; i++ {
		if admitErr(admission, "user-"+strconv.Itoa(i)) == nil {
			admitted = append(admitted, clock.t)
		}
		clock.advance(500 * time.Millisecond)
	}

	if got := maxInWindow(admitted, time.Minute); got > githubContentPerMinute {
		t.Fatalf("admitted %d issues in one minute, GitHub allows %d", got, githubContentPerMinute)
	}
	if got := maxInWindow(admitted, time.Hour); got > githubContentPerHour {
		t.Fatalf("admitted %d issues in one hour, GitHub allows %d", got, githubContentPerHour)
	}
}

// TestDefaultSubmissionLimits_OneUserGetsASmallSliceOfTheHour grounds the
// per-user figure: one account hammering all hour is held to a tenth of
// GitHub's hourly content limit, leaving the rest for everyone else.
func TestDefaultSubmissionLimits_OneUserGetsASmallSliceOfTheHour(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	admission := newSubmissionAdmission(DefaultSubmissionLimits, clock.now)

	var admitted int
	for i := 0; i < 60*60; i++ {
		if admitErr(admission, "one-user") == nil {
			admitted++
		}
		clock.advance(time.Second)
	}
	if limit := githubContentPerHour / 10; admitted > limit {
		t.Fatalf("one user got %d issues in an hour, want at most %d", admitted, limit)
	}
}

// admitErr admits one submission for key and keeps only the refusal, for tests
// that never refund the slot.
func admitErr(a *submissionAdmission, key string) error {
	_, err := a.admit(key)
	return err
}

// TestDefaultSubmissionLimits_FailingFloodStaysUnderGitHubContentLimits is the
// abuse case for refunds (#1115): during an outage every create fails and asks
// for its slot back, so a naive refund would let a flood call GitHub without
// limit. The same worst-case flood, every attempt refunded, must still keep the
// attempts that reach GitHub under its documented content limits, and one user
// hammering through the outage must still get only a small slice of the hour.
func TestDefaultSubmissionLimits_FailingFloodStaysUnderGitHubContentLimits(t *testing.T) {
	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	admission := newSubmissionAdmission(DefaultSubmissionLimits, clock.now)

	var attempts []time.Time
	for i := 0; i < 2*60*60*2; i++ {
		if slot, err := admission.admit("user-" + strconv.Itoa(i)); err == nil {
			attempts = append(attempts, clock.t)
			admission.refund(slot)
		}
		clock.advance(500 * time.Millisecond)
	}
	if got := maxInWindow(attempts, time.Minute); got > githubContentPerMinute {
		t.Fatalf("a failing flood reached GitHub %d times in one minute, GitHub allows %d", got, githubContentPerMinute)
	}
	if got := maxInWindow(attempts, time.Hour); got > githubContentPerHour {
		t.Fatalf("a failing flood reached GitHub %d times in one hour, GitHub allows %d", got, githubContentPerHour)
	}

	clock = &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	admission = newSubmissionAdmission(DefaultSubmissionLimits, clock.now)
	var userAttempts int
	for i := 0; i < 60*60; i++ {
		if slot, err := admission.admit("one-user"); err == nil {
			userAttempts++
			admission.refund(slot)
		}
		clock.advance(time.Second)
	}
	if limit := githubContentPerHour / 10; userAttempts > limit {
		t.Fatalf("one user reached GitHub %d times in an hour of failures, want at most %d", userAttempts, limit)
	}
}

func TestAdmission_RefundHandsBackTheSlotWithinHalfEachCap(t *testing.T) {
	a, _ := newTestAdmission()

	slot, err := a.admit("someone")
	if err != nil {
		t.Fatalf("admit: %v", err)
	}
	if !a.refund(slot) {
		t.Fatal("the first failed create was not refunded")
	}
	if len(a.global.times) != 0 || len(a.users) != 0 {
		t.Fatalf("refund left quota spent: global=%d users=%d", len(a.global.times), len(a.users))
	}
	if a.refund(slot) {
		t.Fatal("refunding the same slot twice handed back quota it no longer held")
	}

	// testLimits.PerUser is 3, so one user may refund only one slot per window.
	second, _ := a.admit("someone")
	if a.refund(second) {
		t.Fatal("a second refund for one user exceeded half the per-user cap")
	}
	if len(a.users["someone"].times) != 1 {
		t.Fatalf("an unrefunded failure left %d user slots, want 1", len(a.users["someone"].times))
	}
}

// maxInWindow returns the most timestamps falling in any half-open window of
// the given length; times must be ascending.
func maxInWindow(times []time.Time, window time.Duration) int {
	best, lo := 0, 0
	for hi := range times {
		for times[hi].Sub(times[lo]) >= window {
			lo++
		}
		best = max(best, hi-lo+1)
	}
	return best
}

// throttleErr stands in for a tracker failure carrying GitHub's rate-limit signal.
type throttleErr struct {
	backoff   time.Duration
	throttled bool
}

func (e throttleErr) Error() string                    { return "tracker throttled" }
func (e throttleErr) Throttled() (time.Duration, bool) { return e.backoff, e.throttled }

func newTestAdmission() (*submissionAdmission, *fakeClock) {
	clock := &fakeClock{t: time.Date(2026, 9, 15, 0, 0, 0, 0, time.UTC)}
	return newSubmissionAdmission(testLimits, clock.now), clock
}

// pauseLength steps the clock a second at a time until admission admits again
// and reports how long the pause lasted.
func pauseLength(t *testing.T, a *submissionAdmission, clock *fakeClock) time.Duration {
	t.Helper()
	start := clock.t
	for admitErr(a, "probe-"+strconv.FormatInt(clock.t.Unix(), 10)) != nil {
		if clock.t.Sub(start) > 2*throttlePauseCeiling {
			t.Fatal("admission never resumed")
		}
		clock.advance(time.Second)
	}
	return clock.t.Sub(start)
}

func TestAdmission_ThrottleRefusesWithBusyCodeWithoutRecording(t *testing.T) {
	a, _ := newTestAdmission()
	a.observe(context.Background(), throttleErr{backoff: 90 * time.Second, throttled: true})

	assertThrottled(t, admitErr(a, "someone"), ErrTrackerPaused)
	if ErrTrackerPaused.ErrorCode() != ErrGlobalReportLimit.ErrorCode() {
		t.Fatalf("paused code = %q, want the busy code %q", ErrTrackerPaused.ErrorCode(), ErrGlobalReportLimit.ErrorCode())
	}
	if len(a.global.times) != 0 || len(a.users) != 0 {
		t.Fatalf("a refused admission spent quota: global=%d users=%d", len(a.global.times), len(a.users))
	}
}

func TestAdmission_ThrottlePauseHonoursHintAboveTheFloor(t *testing.T) {
	a, clock := newTestAdmission()
	a.observe(context.Background(), throttleErr{backoff: 5 * time.Second, throttled: true})
	if got := pauseLength(t, a, clock); got != throttlePauseFloor {
		t.Fatalf("a short hint paused %v, want the %v floor", got, throttlePauseFloor)
	}

	a, clock = newTestAdmission()
	a.observe(context.Background(), throttleErr{backoff: 7 * time.Minute, throttled: true})
	if got := pauseLength(t, a, clock); got != 7*time.Minute {
		t.Fatalf("a 7m hint paused %v, want 7m", got)
	}
}

func TestAdmission_RepeatedThrottlesBackOffExponentiallyUpToTheCeiling(t *testing.T) {
	a, clock := newTestAdmission()
	want := []time.Duration{
		time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute,
		16 * time.Minute, 32 * time.Minute, time.Hour, time.Hour,
	}
	for i, w := range want {
		a.observe(context.Background(), throttleErr{throttled: true})
		if got := pauseLength(t, a, clock); got != w {
			t.Fatalf("throttle %d paused %v, want %v", i+1, got, w)
		}
	}
}

// TestAdmission_InFlightThrottlesDuringAPauseDoNotEscalate covers calls admitted
// before the lockout that all fail at once: they belong to one throttle event,
// so they extend the pause only as far as GitHub asks, without new strikes.
func TestAdmission_InFlightThrottlesDuringAPauseDoNotEscalate(t *testing.T) {
	a, clock := newTestAdmission()
	for i := 0; i < 10; i++ {
		a.observe(context.Background(), throttleErr{throttled: true})
	}
	if got := pauseLength(t, a, clock); got != throttlePauseFloor {
		t.Fatalf("a burst of in-flight throttles paused %v, want the %v floor", got, throttlePauseFloor)
	}

	a, clock = newTestAdmission()
	a.observe(context.Background(), throttleErr{throttled: true})
	a.observe(context.Background(), throttleErr{backoff: 3 * time.Minute, throttled: true})
	if got := pauseLength(t, a, clock); got != 3*time.Minute {
		t.Fatalf("a longer hint during a pause paused %v, want 3m", got)
	}
}

func TestAdmission_SuccessResetsTheBackoff(t *testing.T) {
	a, clock := newTestAdmission()
	a.observe(context.Background(), throttleErr{throttled: true})
	pauseLength(t, a, clock)
	a.observe(context.Background(), nil)
	a.observe(context.Background(), throttleErr{throttled: true})
	if got := pauseLength(t, a, clock); got != throttlePauseFloor {
		t.Fatalf("throttle after a success paused %v, want the %v floor", got, throttlePauseFloor)
	}
}

func TestAdmission_FarFutureHintIsCapped(t *testing.T) {
	a, clock := newTestAdmission()
	a.observe(context.Background(), throttleErr{backoff: 30 * 24 * time.Hour, throttled: true})
	if got := pauseLength(t, a, clock); got != throttlePauseCeiling {
		t.Fatalf("a 30-day hint paused %v, want the %v ceiling", got, throttlePauseCeiling)
	}
}

func TestAdmission_NonThrottleFailuresDoNotPause(t *testing.T) {
	a, _ := newTestAdmission()
	a.observe(context.Background(), errors.New("github is down"))
	a.observe(context.Background(), throttleErr{backoff: time.Hour, throttled: false})
	if err := admitErr(a, "someone"); err != nil {
		t.Fatalf("a non-throttle failure paused admission: %v", err)
	}
}

func TestAdmission_TrackerThrottlePausesFurtherCreates(t *testing.T) {
	throttled := throttleErr{backoff: 2 * time.Minute, throttled: true}
	tracker := &recordingTracker{err: fmt.Errorf("github issues: %w", throttled)}
	svc, clock := throttledService(tracker)

	_, _ = svc.Execute(context.Background(), newUser(), validInput())
	tracker.err = nil
	_, err := svc.Execute(context.Background(), newUser(), validInput())
	assertThrottled(t, err, ErrTrackerPaused)
	if len(tracker.reports) != 0 {
		t.Fatalf("tracker saw %d reports during its lockout, want 0", len(tracker.reports))
	}

	clock.advance(2 * time.Minute)
	if _, err := svc.Execute(context.Background(), newUser(), validInput()); err != nil {
		t.Fatalf("report after the pause was refused: %v", err)
	}
}
