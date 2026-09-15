package domainquality

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"fmt"
	"html/template"
	"sort"
	"strings"
)

// renderBody assembles the panel fragment: the eval-meter score (vs baseline),
// the acquisition success rate, and the bounded history. Every dynamic part — all
// of it watched-app data — is HTML-escaped in the helpers below, so a hostile
// go-api response can never inject markup into the trusted panel HTML.
func renderBody(eval *goapi.EvalStatus, evalStale bool, acq *goapi.AcquisitionStatus, acqStale bool, disco *goapi.DiscographyQuality, discoStale bool, history []core.Signal) template.HTML {
	var sb strings.Builder
	sb.WriteString(evalBlock(eval, evalStale))
	sb.WriteString(acqBlock(acq, acqStale))
	sb.WriteString(discoBlock(disco, discoStale))
	sb.WriteString(historyList(history))
	return template.HTML(sb.String()) //nolint:gosec // every dynamic part escaped in the helpers
}

// evalBlock renders the eval-meter score against its baseline. When the read is
// currently unreachable it flags the block STALE while still showing the
// last-known score — degrade, don't go dark. A side that has never once
// succeeded but is currently failing renders a distinct "unreachable, never
// mirrored" state so a source that fails from startup is not mistaken for one
// that simply has not been polled yet.
func evalBlock(eval *goapi.EvalStatus, stale bool) string {
	if eval == nil {
		if stale {
			return `<p class="empty">STALE — eval unreachable, never mirrored</p>`
		}
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
// STALE when the read is currently unreachable while showing the last-known rate,
// and renders a distinct "unreachable, never mirrored" state when the side has
// never once succeeded but is currently failing.
func acqBlock(acq *goapi.AcquisitionStatus, stale bool) string {
	if acq == nil {
		if stale {
			return `<p class="empty">STALE — acquisition unreachable, never mirrored</p>`
		}
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

// discoBlock renders the Discography category: the served worst-case list, each
// row showing the artist (ref), release count, contamination-suspect count and
// the provider-by-provider split. Like the other blocks it flags STALE when the
// read is currently unreachable while showing the last-known cases. Every dynamic
// field — artist ref, provider names, counts — is watched-app data and is
// HTML-escaped, so a hostile go-api response cannot inject markup. A case is
// framed as a "suspect" with its evidence, never asserted as "wrong": the
// cross-provider disagreement is a hint for the owner to judge.
func discoBlock(disco *goapi.DiscographyQuality, stale bool) string {
	if disco == nil {
		if stale {
			return `<p class="empty">STALE — discography quality unreachable, never mirrored</p>`
		}
		return `<p class="empty">no discography quality mirrored yet</p>`
	}
	var sb strings.Builder
	if stale {
		sb.WriteString(`<p class="empty">STALE — discography quality unreachable, showing last-known cases</p>`)
	} else {
		sb.WriteString(`<p>Discography (contamination suspects, mirrored from /admin/quality/discography)</p>`)
	}
	if len(disco.Cases) == 0 {
		sb.WriteString(`<p class="empty">no discography cases in window</p>`)
		return sb.String()
	}
	sb.WriteString("<ul>")
	for _, c := range disco.Cases {
		sb.WriteString(discoCaseRow(c))
	}
	sb.WriteString("</ul>")
	return sb.String()
}

// discoCaseRow renders one artist's case. artistLabel prefers the display name,
// falling back to the ref; both are escaped.
func discoCaseRow(c goapi.DiscographyCase) string {
	label := c.Artist
	if label == "" {
		label = c.ArtistRef
	}
	ident := label
	if c.ArtistRef != "" && c.ArtistRef != label {
		ident = label + " (" + c.ArtistRef + ")"
	}
	line := fmt.Sprintf("%s — %d releases, %d contamination suspect(s) [%s]",
		ident, c.Releases, c.SingleProvider, providerSplit(c.ProviderCounts))
	return "<li>" + template.HTMLEscapeString(line) + "</li>"
}

// providerSplit renders the per-provider release counts in a stable, sorted order
// so the same case always renders identically. The whole string is escaped by the
// caller.
func providerSplit(counts map[string]int) string {
	if len(counts) == 0 {
		return "no provider split"
	}
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Strings(names)
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s:%d", name, counts[name]))
	}
	return strings.Join(parts, " ")
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
