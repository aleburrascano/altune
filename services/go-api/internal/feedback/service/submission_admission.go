package service

import (
	"altune/go-api/internal/feedback/ports"
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"
)

// SubmissionLimits bounds how often reports may reach the issue tracker. Every
// report spends the one app-wide GitHub token, and GitHub throttles issue
// creation per token, so a per-user cap (one account cannot hog it), a global
// burst cap, and a global sustained cap apply before IssueTracker.Create is
// called. A zero GlobalSustained disables the sustained cap.
type SubmissionLimits struct {
	PerUser               int
	PerUserWindow         time.Duration
	Global                int
	GlobalWindow          time.Duration
	GlobalSustained       int
	GlobalSustainedWindow time.Duration
}

// DefaultSubmissionLimits is sized against GitHub's documented secondary rate
// limits for content-generating requests, "no more than 80 content-generating
// requests per minute and no more than 500 content-generating requests per hour"
// (https://docs.github.com/en/rest/using-the-rest-api/rate-limits-for-the-rest-api#about-secondary-rate-limits):
//   - Global 30/minute is under half the 80/minute ceiling.
//   - GlobalSustained 250/hour is half the 500/hour ceiling; the minute cap alone
//     would admit 1800/hour.
//   - PerUser 5 per 10 minutes is at most 30/hour, so one account holds a small
//     slice of the hourly budget while its back-to-back reports still flow.
//
// TestDefaultSubmissionLimits_StayUnderGitHubContentLimits replays a worst-case
// flood against these figures and fails if they drift above GitHub's limits.
var DefaultSubmissionLimits = SubmissionLimits{
	PerUser:               5,
	PerUserWindow:         10 * time.Minute,
	Global:                30,
	GlobalWindow:          time.Minute,
	GlobalSustained:       250,
	GlobalSustainedWindow: time.Hour,
}

// Bounds on how long a tracker throttle pauses admissions. GitHub says to wait
// at least a minute when it gives no hint and to back off exponentially on
// repeated limits; the ceiling matches its hourly primary-limit window so a
// bogus or far-future hint cannot silence feedback indefinitely.
const (
	throttlePauseFloor   = time.Minute
	throttlePauseCeiling = time.Hour
)

// rateLimitError carries a stable error code and HTTP status so the throttle
// routes through httputil.HandleServiceError; the literal status avoids
// importing net/http, which the application layer forbids.
type rateLimitError struct {
	msg  string
	code string
}

func (e *rateLimitError) Error() string     { return e.msg }
func (e *rateLimitError) HTTPStatus() int   { return 429 }
func (e *rateLimitError) ErrorCode() string { return e.code }

var (
	ErrUserReportLimit = &rateLimitError{
		msg:  "too many reports, try again later",
		code: "feedback.rate_limited",
	}
	ErrGlobalReportLimit = &rateLimitError{
		msg:  "feedback is busy, try again later",
		code: "feedback.busy",
	}
	// ErrTrackerPaused refuses a report while the issue tracker's own rate limit
	// is cooling down. It shares ErrGlobalReportLimit's wire code (the client
	// sees the same "busy") but keeps a distinct message for the throttle log.
	ErrTrackerPaused = &rateLimitError{
		msg:  "feedback is paused while the issue tracker cools down, try again later",
		code: "feedback.busy",
	}
)

// submissionAdmission is a sliding-window log per user plus global burst and
// sustained logs, and a pause set when the tracker itself reports a throttle.
// Memory is bounded: each log holds at most its limit, and idle users are
// pruned once their window has passed.
type submissionAdmission struct {
	mu          sync.Mutex
	limits      SubmissionLimits
	now         func() time.Time
	users       map[string][]time.Time
	global      []time.Time
	sustained   []time.Time
	pausedUntil time.Time
	strikes     int // consecutive tracker throttles since the last success

	// Refund logs: when each handed-back slot was refunded, per user and
	// globally, bounding how many failed creates may skip the quota.
	userRefunds      map[string][]time.Time
	globalRefunds    []time.Time
	sustainedRefunds []time.Time
}

func newSubmissionAdmission(limits SubmissionLimits, now func() time.Time) *submissionAdmission {
	return &submissionAdmission{
		limits:      limits,
		now:         now,
		users:       make(map[string][]time.Time),
		userRefunds: make(map[string][]time.Time),
	}
}

// quotaSlot identifies one admitted submission: whose logs it was recorded in
// and at which instant, so refund can hand back exactly that entry.
type quotaSlot struct {
	key string
	at  time.Time
}

// admit records one submission for key and returns its slot, or returns the
// limit it would breach without recording anything.
func (a *submissionAdmission) admit(key string) (quotaSlot, error) {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()
	if now.Before(a.pausedUntil) {
		return quotaSlot{}, ErrTrackerPaused
	}
	a.pruneUsers(now)
	var user []time.Time
	if logged, ok := a.users[key]; ok && len(logged) > 0 {
		user = recent(logged, now, a.limits.PerUserWindow)
	}
	a.global = recent(a.global, now, a.limits.GlobalWindow)
	a.sustained = recent(a.sustained, now, a.limits.GlobalSustainedWindow)

	if len(user) >= a.limits.PerUser {
		a.users[key] = user
		return quotaSlot{}, ErrUserReportLimit
	}
	if a.globalFull() {
		if len(user) > 0 {
			a.users[key] = user
		}
		return quotaSlot{}, ErrGlobalReportLimit
	}
	a.users[key] = append(user, now)
	a.global = append(a.global, now)
	a.sustained = append(a.sustained, now)
	return quotaSlot{key: key, at: now}, nil
}

func (a *submissionAdmission) globalFull() bool {
	if len(a.global) >= a.limits.Global {
		return true
	}
	return a.limits.GlobalSustained > 0 && len(a.sustained) >= a.limits.GlobalSustained
}

// observe feeds a tracker.Create outcome back into admission. A success clears
// the throttle strikes; a tracker throttle pauses every admission for the wait
// the tracker asked for, never less than an exponentially growing floor, capped
// at throttlePauseCeiling. Any other failure leaves admission unchanged.
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

// pause escalates only on a throttle that arrives outside an active pause:
// in-flight calls admitted before the lockout all fail together, and counting
// each as a fresh strike would turn one burst into an hour-long silence.
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

// strikeFloor records a new strike and returns its exponential minimum pause,
// or returns zero without a strike when a pause is already running.
func (a *submissionAdmission) strikeFloor(now time.Time) time.Duration {
	if now.Before(a.pausedUntil) {
		return 0
	}
	floor := throttlePauseFloor << min(a.strikes, 6)
	a.strikes++
	return floor
}

func (a *submissionAdmission) pruneUsers(now time.Time) {
	for k, times := range a.users {
		if len(recent(times, now, a.limits.PerUserWindow)) == 0 {
			delete(a.users, k)
		}
	}
	for k, times := range a.userRefunds {
		if len(recent(times, now, a.limits.PerUserWindow)) == 0 {
			delete(a.userRefunds, k)
		}
	}
}

// refund hands back a slot admit recorded, after a tracker create that failed
// without creating anything, so an outage does not spend quota on issues that
// never existed. It reports whether it refunded.
//
// Every failed attempt still reached (or tried to reach) the tracker, and
// GitHub's content limits count requests, so refunds are capped: in any window
// at most half of that window's cap may be refunded. Attempts therefore stay
// within 1.5x each cap (45/minute and 375/hour against GitHub's 80 and 500 with
// the defaults, 7 per user per 10 minutes), however many failures a caller or
// an outage produces; past the refund budget a failure spends its slot as
// before. A throttle is never refunded (see refundable), so the pause holds.
// A slot already aged out of every window has nothing to hand back.
func (a *submissionAdmission) refund(slot quotaSlot) bool {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()
	if !a.refundBudgetLeft(slot.key, now) || !a.handBack(slot) {
		return false
	}
	a.userRefunds[slot.key] = append(a.userRefunds[slot.key], now)
	a.globalRefunds = append(a.globalRefunds, now)
	a.sustainedRefunds = append(a.sustainedRefunds, now)
	return true
}

// refundBudgetLeft prunes the refund logs to their windows and reports whether
// key, the burst window and the sustained window each have refunds to spare.
func (a *submissionAdmission) refundBudgetLeft(key string, now time.Time) bool {
	a.userRefunds[key] = recent(a.userRefunds[key], now, a.limits.PerUserWindow)
	if len(a.userRefunds[key]) == 0 {
		delete(a.userRefunds, key)
	}
	a.globalRefunds = recent(a.globalRefunds, now, a.limits.GlobalWindow)
	a.sustainedRefunds = recent(a.sustainedRefunds, now, a.limits.GlobalSustainedWindow)
	sustainedSpent := a.limits.GlobalSustained > 0 &&
		refundBudgetSpent(len(a.sustainedRefunds), a.limits.GlobalSustained)
	return !refundBudgetSpent(len(a.userRefunds[key]), a.limits.PerUser) &&
		!refundBudgetSpent(len(a.globalRefunds), a.limits.Global) &&
		!sustainedSpent
}

// handBack removes the slot's entry from each log still holding it and reports
// whether any did.
func (a *submissionAdmission) handBack(slot quotaSlot) bool {
	user, inUser := without(a.users[slot.key], slot.at)
	if len(user) == 0 {
		delete(a.users, slot.key)
	} else {
		a.users[slot.key] = user
	}
	var inGlobal, inSustained bool
	a.global, inGlobal = without(a.global, slot.at)
	a.sustained, inSustained = without(a.sustained, slot.at)
	return inUser || inGlobal || inSustained
}

// refundBudgetSpent reports whether refunded already holds half of limit, the
// most one window may hand back.
func refundBudgetSpent(refunded, limit int) bool {
	return refunded >= limit/2
}

// without returns times minus one entry equal to at, and whether it found one.
func without(times []time.Time, at time.Time) ([]time.Time, bool) {
	for i, t := range times {
		if t.Equal(at) {
			return append(times[:i:i], times[i+1:]...), true
		}
	}
	return times, false
}

// recent returns a fresh slice of the timestamps still inside the window ending
// at now; it never reslices its argument, so a missing map entry is harmless.
func recent(times []time.Time, now time.Time, window time.Duration) []time.Time {
	kept := make([]time.Time, 0, len(times))
	for _, t := range times {
		if now.Sub(t) < window {
			kept = append(kept, t)
		}
	}
	return kept
}
