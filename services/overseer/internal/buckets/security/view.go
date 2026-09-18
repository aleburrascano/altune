package security

import (
	"altune/overseer/internal/core"
	"fmt"
	"time"
)

// maxErrLen caps a reflected probe error so a long transport error (which can
// carry a probe target) cannot bloat the JSON payload. React escapes the value on
// render, so the cap is about size, not safety.
const maxErrLen = 200

// Data is the security panel payload: the overall self-test verdict, the
// per-check outcomes, and the bounded run history. Any reflected error text is
// watched-app data carried raw (but length-capped); React escapes it.
type Data struct {
	HasRun  bool          `json:"hasRun"`
	Passed  int           `json:"passed"`
	Total   int           `json:"total"`
	LastRun time.Time     `json:"lastRun"`
	Checks  []CheckView   `json:"checks"`
	History []core.Signal `json:"history"`
}

// CheckView is one check's outcome for the panel. Reached=false means the check
// could not run (transport failure or the fence refusing an off-allowlist
// target), which is what drives degrade-to-stale rather than a fail.
type CheckView struct {
	Name    string `json:"name"`
	Desc    string `json:"desc"`
	Reached bool   `json:"reached"`
	Passed  bool   `json:"passed"`
	Status  int    `json:"status"`
	Error   string `json:"error,omitempty"`
}

// suiteData shapes a suite result into the exported, JSON-tagged payload. A nil
// result yields a zero-value HasRun=false payload the panel renders as "no run
// yet".
func suiteData(last *suiteResult) Data {
	if last == nil {
		return Data{}
	}
	checks := make([]CheckView, 0, len(last.results))
	for _, r := range last.results {
		cv := CheckView{Name: r.name, Desc: r.desc, Reached: r.reached(), Passed: r.passed, Status: r.status}
		if r.err != nil {
			cv.Error = shorten(r.err.Error())
		}
		checks = append(checks, cv)
	}
	return Data{
		HasRun:  true,
		Passed:  last.passed(),
		Total:   last.total(),
		LastRun: last.at,
		Checks:  checks,
	}
}

// securityHealth grades the last self-test run, hoisting the panel's own verdict
// (web/src/panels/security.panel.tsx): a check that reached go-api and did not get
// a rejection is a regression in hardening we hold today, which is critical. A run
// with unreached checks proves nothing about those defenses, and a suite that has
// never run proves nothing at all — both warn rather than report a green all-clear
// nobody measured.
func securityHealth(last *suiteResult) (core.Severity, string) {
	if last == nil || last.total() == 0 {
		return core.SeverityWarn, "no self-test run yet"
	}
	failing, unreached := last.failing(), last.unreached()
	switch {
	case failing > 0:
		return core.SeverityCritical, fmt.Sprintf("regression detected — %d of %d self-tests failing", failing, last.total())
	case unreached > 0:
		return core.SeverityWarn, fmt.Sprintf("partial run — %d of %d self-tests reached", last.total()-unreached, last.total())
	default:
		return core.SeverityOK, fmt.Sprintf("all defenses held — %d/%d self-tests passed", last.passed(), last.total())
	}
}

// shorten caps a reflected error string so a long transport error cannot bloat
// the payload.
func shorten(s string) string {
	if len(s) > maxErrLen {
		return s[:maxErrLen] + "…"
	}
	return s
}

// summarySignal folds one suite run into the shared signal shape for the bounded
// history.
func summarySignal(res suiteResult) core.Signal {
	return core.Signal{
		At:   res.at,
		Kind: "selftest",
		Text: fmt.Sprintf("security self-test %d/%d passed", res.passed(), res.total()),
	}
}
