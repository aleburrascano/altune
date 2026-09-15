package security

import (
	"altune/overseer/internal/core"
	"fmt"
	"html/template"
	"strings"
	"time"
)

// renderBody assembles the panel fragment: the overall verdict with last-run
// (flagged STALE when go-api is currently unreachable), the per-check pass/fail
// list, and the bounded history. Every dynamic part — statuses and any reflected
// error text from a probe — is HTML-escaped in the helpers below, so nothing a
// probe reflects can inject markup into the trusted panel HTML.
func renderBody(last *suiteResult, stale bool, history []core.Signal) template.HTML {
	if last == nil {
		return template.HTML(`<p class="empty">no security self-test has run yet</p>`) //nolint:gosec // constant literal
	}
	var sb strings.Builder
	sb.WriteString(verdictLine(*last, stale))
	sb.WriteString(checkList(last.results))
	sb.WriteString(historyList(history))
	return template.HTML(sb.String()) //nolint:gosec // every dynamic part escaped in the helpers
}

// verdictLine renders the overall pass/fail summary and the last-run time. When
// the latest run could not reach go-api it flags the verdict STALE while still
// showing the last-known result — degrade, don't go dark.
func verdictLine(last suiteResult, stale bool) string {
	summary := fmt.Sprintf("%d/%d checks passed · last run %s",
		last.passed(), last.total(), last.at.Format(time.RFC3339))
	if stale {
		return `<p class="empty">STALE — go-api unreachable, showing last-known self-test (` +
			template.HTMLEscapeString(summary) + `)</p>`
	}
	if last.passed() == last.total() {
		return "<p>PASS — " + template.HTMLEscapeString(summary) + "</p>"
	}
	return `<p class="down">FAIL — ` + template.HTMLEscapeString(summary) + "</p>"
}

// checkList renders each check's verdict. The description is a fixed literal and
// the status an int, but any reflected error text (a fence refusal or a
// transport error, which can carry a probe target) is HTML-escaped so it can
// never inject markup.
func checkList(results []checkResult) string {
	var sb strings.Builder
	sb.WriteString("<ul>")
	for _, r := range results {
		sb.WriteString("<li>" + template.HTMLEscapeString(r.desc) + " — " + verdict(r) + "</li>")
	}
	sb.WriteString("</ul>")
	return sb.String()
}

// verdict renders one check's outcome: an unreached check shows why (escaped),
// a reached check shows PASS/FAIL and the status it saw.
func verdict(r checkResult) string {
	if !r.reached() {
		return `<span class="empty">could not run (` + template.HTMLEscapeString(shorten(r.err.Error())) + ")</span>"
	}
	if r.passed {
		return template.HTMLEscapeString(fmt.Sprintf("PASS (status %d)", r.status))
	}
	return `<span class="down">` + template.HTMLEscapeString(fmt.Sprintf("FAIL (status %d)", r.status)) + "</span>"
}

// historyList renders the bounded self-test history, each summary HTML-escaped
// since it is built from run outcomes.
func historyList(history []core.Signal) string {
	if len(history) == 0 {
		return `<p class="empty">no self-test history yet</p>`
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "<p>%d self-test run(s)</p><ul>", len(history))
	for _, s := range history {
		sb.WriteString("<li>" + template.HTMLEscapeString(s.Text) + "</li>")
	}
	sb.WriteString("</ul>")
	return sb.String()
}

// shorten caps a reflected error string so a long transport error cannot bloat
// the panel; the escape still runs on the capped value.
func shorten(s string) string {
	const maxLen = 200
	if len(s) > maxLen {
		return s[:maxLen] + "…"
	}
	return s
}

// summarySignal folds one suite run into the shared signal shape for the bounded
// history. The text is built from the run's own counts, escaped at render time.
func summarySignal(res suiteResult) core.Signal {
	return core.Signal{
		At:   res.at,
		Kind: "selftest",
		Text: fmt.Sprintf("security self-test %d/%d passed", res.passed(), res.total()),
	}
}
