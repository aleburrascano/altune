package security

import (
	"context"
	"net/http"
	"time"
)

// check is one safe self-test: a name, a human description, the read path (with
// an optional malformed query) it GETs, and the set of statuses that count as
// the app correctly rejecting the probe. A check PASSES only when the app
// answers with a rejection status; a 2xx (the defense let the probe through) or
// a 5xx (the app fell over) fails it. Every check is a read/rejection assertion,
// never a mutation — the prober only ever issues GETs.
type check struct {
	name       string
	desc       string
	path       string
	rawQuery   string
	wantReject []int
	// burst fires the same probe repeatedly to prove a rate limit sheds load; a
	// zero or one means a single probe.
	burst int
}

// accepts reports whether status is one of the check's acceptable rejection
// statuses. A status outside the set — notably any 2xx or 5xx — is a failure.
func (c check) accepts(status int) bool {
	for _, s := range c.wantReject {
		if status == s {
			return true
		}
	}
	return false
}

// reps is the number of times the check fires, at least one.
func (c check) reps() int {
	if c.burst < 1 {
		return 1
	}
	return c.burst
}

// defaultSuite is the initial safe check set, grounded in go-api's surface
// (internal/app/routes.go): open /health, JWT /v1/*, observe /observe/*, and the
// rate-limited discovery routes. Each is a read or a rejection assertion; none
// mutates state.
func defaultSuite() []check {
	return []check{
		{
			name: "unauth-v1", desc: "unauthenticated /v1 read is rejected",
			path: "/v1/library", wantReject: rejectAuth(),
		},
		{
			name: "observe-gate", desc: "unauthenticated /observe read is rejected",
			path: "/observe/health", wantReject: rejectAuth(),
		},
		{
			name: "rate-limit-burst", desc: "a burst is shed or rejected, never served",
			path: "/v1/discovery/search", rawQuery: "q=overseer-selftest",
			wantReject: rejectBurst(), burst: 8,
		},
		{
			name: "bad-input", desc: "a malformed read is rejected cleanly, not a 500",
			path: "/v1/discovery/search", rawQuery: "q=%zz%00&limit=not-a-number",
			wantReject: rejectInput(),
		},
	}
}

// rejectAuth is the acceptable rejection set for an unauthenticated or
// non-operator read: either the auth layer (401) or the operator gate (403).
func rejectAuth() []int {
	return []int{http.StatusUnauthorized, http.StatusForbidden}
}

// rejectBurst additionally accepts 429: a burst that reaches a rate limiter is
// shed with Too Many Requests, which is exactly the defense holding.
func rejectBurst() []int {
	return []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests}
}

// rejectInput is the acceptable set for a malformed read: any clean 4xx. The
// point of the check is that bad input is rejected, never a 500 or a served 2xx.
func rejectInput() []int {
	return []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusUnprocessableEntity,
	}
}

// checkResult is one check's verdict. err set means the check could not run
// (a transport failure or the fence refusing an off-allowlist target); passed
// applies only when the check reached the app and got a status.
type checkResult struct {
	name   string
	desc   string
	status int
	passed bool
	err    error
}

// reached reports whether the check actually reached go-api. A check that could
// not reach it is neither a pass nor a fail — it drives degrade-to-stale.
func (r checkResult) reached() bool { return r.err == nil }

// runCheck fires one check (repeating for a burst) and returns its verdict. A
// transport/fence error stops the run and marks the check unreached; a
// non-rejection status (2xx served or 5xx crash) is a genuine regression and
// stops the run as a failure.
func runCheck(ctx context.Context, p prober, c check) checkResult {
	tgt := p.target(c.path, c.rawQuery)
	res := checkResult{name: c.name, desc: c.desc}
	for i := 0; i < c.reps(); i++ {
		out := p.do(ctx, tgt)
		if out.err != nil {
			res.err = out.err
			return res
		}
		res.status = out.status
		if !c.accepts(out.status) {
			res.passed = false
			return res
		}
	}
	res.passed = true
	return res
}

// suiteResult is one full run of the suite at a point in time.
type suiteResult struct {
	at      time.Time
	results []checkResult
}

// reachedAny reports whether at least one check reached go-api. When none did,
// the whole run is a source outage and the bucket degrades to stale.
func (s suiteResult) reachedAny() bool {
	for _, r := range s.results {
		if r.reached() {
			return true
		}
	}
	return false
}

// passed counts the checks that reached the app and passed.
func (s suiteResult) passed() int {
	n := 0
	for _, r := range s.results {
		if r.reached() && r.passed {
			n++
		}
	}
	return n
}

// failing counts the checks that reached the app and did NOT get a rejection —
// the defense let the probe through or the app fell over. This is the regression
// count, kept distinct from unreached: a check that never landed is not a pass and
// not a fail.
func (s suiteResult) failing() int {
	n := 0
	for _, r := range s.results {
		if r.reached() && !r.passed {
			n++
		}
	}
	return n
}

// unreached counts the checks that could not reach go-api (transport failure or
// the fence refusing an off-allowlist target), so those defenses went unproven.
func (s suiteResult) unreached() int {
	n := 0
	for _, r := range s.results {
		if !r.reached() {
			n++
		}
	}
	return n
}

// total is the number of checks in the run.
func (s suiteResult) total() int { return len(s.results) }

// runSuite runs every check in order and stamps the result with the run time.
func runSuite(ctx context.Context, p prober, checks []check, now func() time.Time) suiteResult {
	res := suiteResult{at: now().UTC()}
	for _, c := range checks {
		res.results = append(res.results, runCheck(ctx, p, c))
	}
	return res
}
