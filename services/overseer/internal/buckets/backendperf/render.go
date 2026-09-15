package backendperf

import (
	"altune/overseer/internal/core"
	"fmt"
	"html/template"
	"strings"
)

// slowestHighlight is how many of the slowest routes the panel calls out
// explicitly above the full table — the "slowest routes highlighted" anchor from
// the brief. The #1 slowest is marked distinctly within that list.
const slowestHighlight = 3

// renderBody assembles the panel fragment: a live/stale status line, the slowest
// routes highlight, the full per-route latency table, and a short throughput
// trend. Every dynamic part — route templates and history text, all watched-app
// data — is HTML-escaped in the helpers below, so a hostile go-api response can
// never inject markup into the trusted panel HTML.
func renderBody(stats []routeStat, throughput []core.Signal, stale bool) template.HTML {
	var sb strings.Builder
	sb.WriteString(statusLine(stale))
	sb.WriteString(slowestSection(stats))
	sb.WriteString(routeTable(stats))
	sb.WriteString(throughputTrend(throughput))
	return template.HTML(sb.String()) //nolint:gosec // every dynamic part escaped in the helpers
}

// statusLine reports whether the latency view is live or serving last-known data
// flagged stale. Its text is a fixed literal, never watched-app data.
func statusLine(stale bool) string {
	if stale {
		return `<p class="empty">STALE — go-api unreachable, showing last-known latency</p>`
	}
	return `<p>LIVE — per-route latency from /admin/metrics/live</p>`
}

// slowestSection highlights the slowest routes by p99. stats arrives sorted
// slowest-first, so the highlight is the head of the list; the #1 slowest is
// flagged with a "slow" class for the panel to emphasise.
func slowestSection(stats []routeStat) string {
	if len(stats) == 0 {
		return `<p class="empty">no route latency yet</p>`
	}
	var sb strings.Builder
	sb.WriteString("<h3>Slowest routes (p99)</h3><ul>")
	for i, s := range stats {
		if i >= slowestHighlight {
			break
		}
		sb.WriteString(slowestItem(s, i == 0))
	}
	sb.WriteString("</ul>")
	return sb.String()
}

// slowestItem renders one highlighted slow route. The route template is
// watched-app data and is HTML-escaped; top marks the single slowest route.
func slowestItem(s routeStat, top bool) string {
	cls := ""
	if top {
		cls = ` class="slow"`
	}
	return fmt.Sprintf("<li%s>%s — p99 %s</li>", cls, escapeRoute(s.Route), fmtMs(s.P99))
}

// routeTable renders the full per-route latency table (throughput + p50/p95/p99),
// each route template HTML-escaped as watched-app data.
func routeTable(stats []routeStat) string {
	if len(stats) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString("<h3>Per-route latency</h3>")
	sb.WriteString("<table><thead><tr><th>route</th><th>reqs</th><th>p50</th><th>p95</th><th>p99</th></tr></thead><tbody>")
	for _, s := range stats {
		fmt.Fprintf(&sb, "<tr><td>%s</td><td>%d</td><td>%s</td><td>%s</td><td>%s</td></tr>",
			escapeRoute(s.Route), s.Count, fmtMs(s.P50), fmtMs(s.P95), fmtMs(s.P99))
	}
	sb.WriteString("</tbody></table>")
	return sb.String()
}

// throughputTrend renders the bounded recent-throughput samples, each sample's
// text HTML-escaped since it is built from watched-app counts.
func throughputTrend(history []core.Signal) string {
	if len(history) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "<h3>Throughput trend</h3><p>%d sample(s)</p><ul>", len(history))
	for _, s := range history {
		sb.WriteString("<li>" + template.HTMLEscapeString(s.Text) + "</li>")
	}
	sb.WriteString("</ul>")
	return sb.String()
}

// escapeRoute HTML-escapes a route template. Route names come from go-api and are
// the watched-app text on this panel; escaping them means a hostile or malformed
// route template can never inject markup into the trusted panel HTML.
func escapeRoute(route string) string {
	return template.HTMLEscapeString(route)
}

// fmtMs renders an estimated percentile latency. An estimate from the unbounded
// +Inf tail is a lower bound, rendered with a leading "≥" so it is never read as
// an exact figure.
func fmtMs(p percentile) string {
	if p.Overflow {
		return fmt.Sprintf("≥%.0fms", p.Ms)
	}
	return fmt.Sprintf("%.1fms", p.Ms)
}
