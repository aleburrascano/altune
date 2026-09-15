package domainquality

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"fmt"
	"html/template"
	"strings"
)

// renderBody assembles the panel fragment: the eval-meter score (vs baseline),
// the acquisition success rate, and the bounded history. Every dynamic part — all
// of it watched-app data — is HTML-escaped in the helpers below, so a hostile
// go-api response can never inject markup into the trusted panel HTML.
func renderBody(eval *goapi.EvalStatus, evalStale bool, acq *goapi.AcquisitionStatus, acqStale bool, history []core.Signal) template.HTML {
	var sb strings.Builder
	sb.WriteString(evalBlock(eval, evalStale))
	sb.WriteString(acqBlock(acq, acqStale))
	sb.WriteString(historyList(history))
	return template.HTML(sb.String()) //nolint:gosec // every dynamic part escaped in the helpers
}

// evalBlock renders the eval-meter score against its baseline. When the read is
// currently unreachable it flags the block STALE while still showing the
// last-known score — degrade, don't go dark.
func evalBlock(eval *goapi.EvalStatus, stale bool) string {
	if eval == nil {
		return `<p class="empty">no eval score mirrored yet</p>`
	}
	var sb strings.Builder
	if stale {
		sb.WriteString(`<p class="empty">STALE — eval unreachable, showing last-known score</p>`)
	} else {
		sb.WriteString(`<p>Search quality (eval meter, mirrored from /admin/eval)</p>`)
	}
	sb.WriteString("<ul>")
	if eval.Scored() {
		line := fmt.Sprintf("score %.2f", *eval.Score)
		if eval.Baseline != nil {
			line += fmt.Sprintf(" vs baseline %.2f", *eval.Baseline)
		}
		sb.WriteString("<li>" + template.HTMLEscapeString(line) + "</li>")
	} else {
		sb.WriteString(`<li>no score yet</li>`)
	}
	sb.WriteString("<li>state: " + template.HTMLEscapeString(eval.State) + "</li>")
	if eval.Error != "" {
		sb.WriteString("<li>error: " + template.HTMLEscapeString(eval.Error) + "</li>")
	}
	sb.WriteString(evalQueries(eval.Queries))
	sb.WriteString("</ul>")
	return sb.String()
}

// evalQueries renders the per-query results. Each query string is watched-app
// data and is HTML-escaped, so a query containing markup cannot inject into the
// panel.
func evalQueries(queries []goapi.EvalQuery) string {
	if len(queries) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<li>queries:<ul>")
	for _, q := range queries {
		verdict := "fail"
		if q.Passed {
			verdict = "pass"
		}
		sb.WriteString("<li>" + template.HTMLEscapeString(q.Query) + " — " + verdict + "</li>")
	}
	sb.WriteString("</ul></li>")
	return sb.String()
}

// acqBlock renders the acquisition success rate. Like the eval block it flags
// STALE when the read is currently unreachable while showing the last-known rate.
func acqBlock(acq *goapi.AcquisitionStatus, stale bool) string {
	if acq == nil {
		return `<p class="empty">no acquisition health mirrored yet</p>`
	}
	var sb strings.Builder
	if stale {
		sb.WriteString(`<p class="empty">STALE — acquisition unreachable, showing last-known rate</p>`)
	} else {
		sb.WriteString(`<p>Acquisition (mirrored from /admin/acquisition)</p>`)
	}
	sb.WriteString("<ul>")
	if rate, ok := acq.SuccessRate(); ok {
		line := fmt.Sprintf("success rate %.0f%% (%d ok / %d failed)", rate*100, acq.Succeeded, acq.Failed)
		sb.WriteString("<li>" + template.HTMLEscapeString(line) + "</li>")
	} else {
		sb.WriteString(`<li>success rate n/a — no completed jobs</li>`)
	}
	gauges := fmt.Sprintf("in-flight %d · queue %d/%d · rejected %d",
		acq.InFlight, acq.QueueDepth, acq.QueueCapacity, acq.Rejected)
	sb.WriteString("<li>" + template.HTMLEscapeString(gauges) + "</li>")
	sb.WriteString("</ul>")
	return sb.String()
}

// historyList renders the bounded domain-quality history, each sample's text
// HTML-escaped since it is built from watched-app data.
func historyList(history []core.Signal) string {
	if len(history) == 0 {
		return `<p class="empty">no quality samples yet</p>`
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "<p>%d quality sample(s)</p><ul>", len(history))
	for _, s := range history {
		sb.WriteString("<li>" + template.HTMLEscapeString(s.Text) + "</li>")
	}
	sb.WriteString("</ul>")
	return sb.String()
}
