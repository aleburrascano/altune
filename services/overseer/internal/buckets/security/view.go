package security

import (
	"altune/overseer/internal/core"
	"fmt"
	"time"
)

const maxErrLen = 200

type Data struct {
	HasRun  bool          `json:"hasRun"`
	Passed  int           `json:"passed"`
	Total   int           `json:"total"`
	LastRun time.Time     `json:"lastRun"`
	Checks  []CheckView   `json:"checks"`
	History []core.Signal `json:"history"`
}

type CheckView struct {
	Name    string `json:"name"`
	Desc    string `json:"desc"`
	Reached bool   `json:"reached"`
	Passed  bool   `json:"passed"`
	Status  int    `json:"status"`
	Error   string `json:"error,omitempty"`
}

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

func shorten(s string) string {
	if len(s) > maxErrLen {
		return s[:maxErrLen] + "…"
	}
	return s
}

func summarySignal(res suiteResult) core.Signal {
	return core.Signal{
		At:   res.at,
		Kind: "selftest",
		Text: fmt.Sprintf("security self-test %d/%d passed", res.passed(), res.total()),
	}
}
