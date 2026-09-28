package service

import (
	"altune/go-api/internal/feedback/ports"
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

type SubmissionLimits struct {
	PerUser               int
	PerUserWindow         time.Duration
	Global                int
	GlobalWindow          time.Duration
	GlobalSustained       int
	GlobalSustainedWindow time.Duration
}

var DefaultSubmissionLimits = SubmissionLimits{
	PerUser:               5,
	PerUserWindow:         10 * time.Minute,
	Global:                30,
	GlobalWindow:          time.Minute,
	GlobalSustained:       250,
	GlobalSustainedWindow: time.Hour,
}

const (
	throttlePauseFloor   = time.Minute
	throttlePauseCeiling = time.Hour
)

type rateLimitError struct {
	msg  string
	code string
}

func (e *rateLimitError) Error() string     { return e.msg }
func (e *rateLimitError) HTTPStatus() int   { return 429 }
func (e *rateLimitError) ErrorCode() string { return e.code }

type retryableLimit struct {
	*rateLimitError
	wait time.Duration
}

func (e *retryableLimit) Unwrap() error             { return e.rateLimitError }
func (e *retryableLimit) RetryAfter() time.Duration { return e.wait }

func withWait(limit *rateLimitError, wait time.Duration) error {
	return &retryableLimit{rateLimitError: limit, wait: wait}
}

var (
	ErrUserReportLimit = &rateLimitError{
		msg:  "too many reports, try again later",
		code: "feedback.rate_limited",
	}
	ErrGlobalReportLimit = &rateLimitError{
		msg:  "feedback is busy, try again later",
		code: "feedback.busy",
	}
	ErrTrackerPaused = &rateLimitError{
		msg:  "feedback is paused while the issue tracker cools down, try again later",
		code: "feedback.busy",
	}
)

type submissionAdmission struct {
	mu          sync.Mutex
	now         func() time.Time
	perUser     windowLog
	users       map[string]windowLog
	global      windowLog
	sustained   windowLog
	pausedUntil time.Time
	strikes     int

	userRefunds      map[string]windowLog
	globalRefunds    windowLog
	sustainedRefunds windowLog
}

func newSubmissionAdmission(limits SubmissionLimits, now func() time.Time) *submissionAdmission {
	global := requiredCap(limits.Global, limits.GlobalWindow)
	sustained := optionalCap(limits.GlobalSustained, limits.GlobalSustainedWindow)
	return &submissionAdmission{
		now:              now,
		perUser:          requiredCap(limits.PerUser, limits.PerUserWindow),
		users:            make(map[string]windowLog),
		global:           global,
		sustained:        sustained,
		userRefunds:      make(map[string]windowLog),
		globalRefunds:    global.refundBudget(),
		sustainedRefunds: sustained.refundBudget(),
	}
}

type quotaSlot struct {
	key string
	at  time.Time
}

func (a *submissionAdmission) admit(key string) (quotaSlot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()
	if now.Before(a.pausedUntil) {
		return quotaSlot{}, withWait(ErrTrackerPaused, a.pausedUntil.Sub(now))
	}
	a.pruneUsers(now)
	user := logFor(a.users, key, a.perUser).recent(now)
	a.global = a.global.recent(now)
	a.sustained = a.sustained.recent(now)

	if user.full() {
		a.users[key] = user
		return quotaSlot{}, withWait(ErrUserReportLimit, user.resetIn(now))
	}
	if a.globalFull() {
		if len(user.times) > 0 {
			a.users[key] = user
		}
		return quotaSlot{}, withWait(ErrGlobalReportLimit, a.globalResetIn(now))
	}
	a.users[key] = user.with(now)
	a.global = a.global.with(now)
	a.sustained = a.sustained.with(now)
	return quotaSlot{key: key, at: now}, nil
}

func (a *submissionAdmission) globalResetIn(now time.Time) time.Duration {
	var wait time.Duration
	for _, log := range []windowLog{a.global, a.sustained} {
		if log.full() {
			wait = max(wait, log.resetIn(now))
		}
	}
	return wait
}

func (a *submissionAdmission) globalFull() bool {
	return a.global.full() || a.sustained.full()
}

func (a *submissionAdmission) observe(ctx context.Context, err error) {
	if err == nil {
		a.mu.Lock()
		a.strikes = 0
		a.mu.Unlock()
		return
	}
	var throttle ports.TrackerThrottle
	if !errors.As(err, &throttle) {
		return
	}
	if backoff, ok := throttle.Throttled(); ok {
		a.mu.Lock()
		defer a.mu.Unlock()
		a.pause(ctx, backoff)
	}
}

func (a *submissionAdmission) pause(ctx context.Context, requested time.Duration) {
	now := a.now()
	until := now.Add(min(max(requested, a.strikeFloor(now)), throttlePauseCeiling))
	if until.After(a.pausedUntil) {
		a.pausedUntil = until
	}
	slog.WarnContext(ctx, "feedback.tracker_throttled",
		"requested_backoff", requested.String(),
		"paused_until", a.pausedUntil,
		"strikes", a.strikes,
	)
}

func (a *submissionAdmission) strikeFloor(now time.Time) time.Duration {
	if now.Before(a.pausedUntil) {
		return 0
	}
	floor := throttlePauseFloor << min(a.strikes, 6)
	a.strikes++
	return floor
}

func (a *submissionAdmission) pruneUsers(now time.Time) {
	pruneIdle(a.users, now)
	pruneIdle(a.userRefunds, now)
}

func pruneIdle(logs map[string]windowLog, now time.Time) {
	for key, log := range logs {
		if len(log.recent(now).times) == 0 {
			delete(logs, key)
		}
	}
}

func (a *submissionAdmission) refund(slot quotaSlot) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()
	if !a.refundBudgetLeft(slot.key, now) || !a.handBack(slot) {
		return false
	}
	a.userRefunds[slot.key] = logFor(a.userRefunds, slot.key, a.perUser.refundBudget()).with(now)
	a.globalRefunds = a.globalRefunds.with(now)
	a.sustainedRefunds = a.sustainedRefunds.with(now)
	return true
}

func (a *submissionAdmission) refundBudgetLeft(key string, now time.Time) bool {
	user := logFor(a.userRefunds, key, a.perUser.refundBudget()).recent(now)
	if len(user.times) == 0 {
		delete(a.userRefunds, key)
	} else {
		a.userRefunds[key] = user
	}
	a.globalRefunds = a.globalRefunds.recent(now)
	a.sustainedRefunds = a.sustainedRefunds.recent(now)
	return !user.full() && !a.globalRefunds.full() && !a.sustainedRefunds.full()
}

func (a *submissionAdmission) handBack(slot quotaSlot) bool {
	user, inUser := logFor(a.users, slot.key, a.perUser).without(slot.at)
	if len(user.times) == 0 {
		delete(a.users, slot.key)
	} else {
		a.users[slot.key] = user
	}
	var inGlobal, inSustained bool
	a.global, inGlobal = a.global.without(slot.at)
	a.sustained, inSustained = a.sustained.without(slot.at)
	return inUser || inGlobal || inSustained
}

const uncapped = -1

type windowLog struct {
	times  []time.Time
	limit  int
	window time.Duration
}

func requiredCap(limit int, window time.Duration) windowLog {
	return windowLog{limit: max(limit, 0), window: window}
}

func optionalCap(limit int, window time.Duration) windowLog {
	if limit <= 0 {
		return windowLog{limit: uncapped, window: window}
	}
	return windowLog{limit: limit, window: window}
}

func (w windowLog) enabled() bool {
	return w.limit != uncapped
}

func (w windowLog) full() bool {
	return w.enabled() && len(w.times) >= w.limit
}

func (w windowLog) resetIn(now time.Time) time.Duration {
	if len(w.times) == 0 {
		return w.window
	}
	return w.times[0].Add(w.window).Sub(now)
}

func (w windowLog) refundBudget() windowLog {
	if !w.enabled() {
		return windowLog{limit: uncapped, window: w.window}
	}
	return windowLog{limit: w.limit / 2, window: w.window}
}

func (w windowLog) recent(now time.Time) windowLog {
	w.times = recent(w.times, now, w.window)
	return w
}

func (w windowLog) with(at time.Time) windowLog {
	w.times = append(w.times, at)
	return w
}

func (w windowLog) without(at time.Time) (windowLog, bool) {
	var found bool
	w.times, found = without(w.times, at)
	return w, found
}

func logFor(logs map[string]windowLog, key string, blank windowLog) windowLog {
	if log, ok := logs[key]; ok {
		return log
	}
	return blank
}

func without(times []time.Time, at time.Time) ([]time.Time, bool) {
	for i, t := range times {
		if t.Equal(at) {
			return append(times[:i:i], times[i+1:]...), true
		}
	}
	return times, false
}

func recent(times []time.Time, now time.Time, window time.Duration) []time.Time {
	kept := make([]time.Time, 0, len(times))
	for _, t := range times {
		if now.Sub(t) < window {
			kept = append(kept, t)
		}
	}
	return kept
}
