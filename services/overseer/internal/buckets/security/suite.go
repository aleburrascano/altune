package security

import (
	"context"
	"net/http"
	"time"
)

type check struct {
	name       string
	desc       string
	path       string
	rawQuery   string
	wantReject []int
	burst      int
}

func (c check) accepts(status int) bool {
	for _, s := range c.wantReject {
		if status == s {
			return true
		}
	}
	return false
}

func (c check) reps() int {
	if c.burst < 1 {
		return 1
	}
	return c.burst
}

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

func rejectAuth() []int {
	return []int{http.StatusUnauthorized, http.StatusForbidden}
}

func rejectBurst() []int {
	return []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests}
}

func rejectInput() []int {
	return []int{
		http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden,
		http.StatusNotFound, http.StatusUnprocessableEntity,
	}
}

type checkResult struct {
	name   string
	desc   string
	status int
	passed bool
	err    error
}

func (r checkResult) reached() bool { return r.err == nil }

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

type suiteResult struct {
	at      time.Time
	results []checkResult
}

func (s suiteResult) reachedAny() bool {
	for _, r := range s.results {
		if r.reached() {
			return true
		}
	}
	return false
}

func (s suiteResult) passed() int {
	n := 0
	for _, r := range s.results {
		if r.reached() && r.passed {
			n++
		}
	}
	return n
}

func (s suiteResult) failing() int {
	n := 0
	for _, r := range s.results {
		if r.reached() && !r.passed {
			n++
		}
	}
	return n
}

func (s suiteResult) unreached() int {
	n := 0
	for _, r := range s.results {
		if !r.reached() {
			n++
		}
	}
	return n
}

func (s suiteResult) total() int { return len(s.results) }

func runSuite(ctx context.Context, p prober, checks []check, now func() time.Time) suiteResult {
	res := suiteResult{at: now().UTC()}
	for _, c := range checks {
		res.results = append(res.results, runCheck(ctx, p, c))
	}
	return res
}
