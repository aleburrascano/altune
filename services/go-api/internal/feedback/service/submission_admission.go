package service

import (
	"sync"
	"time"
)

// SubmissionLimits bounds how often reports may reach the issue tracker. Every
// report spends the one app-wide GitHub token, and GitHub throttles issue
// creation per token, so both a per-user cap (one account cannot hog it) and a
// global cap (an incident-wide burst stays under GitHub's secondary limits)
// apply before IssueTracker.Create is called.
type SubmissionLimits struct {
	PerUser       int
	PerUserWindow time.Duration
	Global        int
	GlobalWindow  time.Duration
}

// DefaultSubmissionLimits keeps a real user's back-to-back reports flowing
// while holding the shared token well below GitHub's content-creation limits.
var DefaultSubmissionLimits = SubmissionLimits{
	PerUser:       5,
	PerUserWindow: 10 * time.Minute,
	Global:        30,
	GlobalWindow:  time.Minute,
}

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
)

// submissionAdmission is a sliding-window log per user plus one global log.
// Memory is bounded: each log holds at most its limit, and idle users are
// pruned once their window has passed.
type submissionAdmission struct {
	mu     sync.Mutex
	limits SubmissionLimits
	now    func() time.Time
	users  map[string][]time.Time
	global []time.Time
}

func newSubmissionAdmission(limits SubmissionLimits, now func() time.Time) *submissionAdmission {
	return &submissionAdmission{limits: limits, now: now, users: make(map[string][]time.Time)}
}

// admit records one submission for key, or returns the limit it would breach
// without recording anything.
func (a *submissionAdmission) admit(key string) error {
	a.mu.Lock()
	defer a.mu.Unlock()

	now := a.now()
	a.pruneUsers(now)
	var user []time.Time
	if logged, ok := a.users[key]; ok && len(logged) > 0 {
		user = recent(logged, now, a.limits.PerUserWindow)
	}
	a.global = recent(a.global, now, a.limits.GlobalWindow)

	if len(user) >= a.limits.PerUser {
		a.users[key] = user
		return ErrUserReportLimit
	}
	if len(a.global) >= a.limits.Global {
		if len(user) > 0 {
			a.users[key] = user
		}
		return ErrGlobalReportLimit
	}
	a.users[key] = append(user, now)
	a.global = append(a.global, now)
	return nil
}

func (a *submissionAdmission) pruneUsers(now time.Time) {
	for k, times := range a.users {
		if len(recent(times, now, a.limits.PerUserWindow)) == 0 {
			delete(a.users, k)
		}
	}
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
