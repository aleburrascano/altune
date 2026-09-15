package cost

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/oci"
	"fmt"
	"html/template"
	"strings"
)

// topServices is how many per-service spend lines the panel lists. The spend is
// grouped by service and largest-first, so this is the top-N cost drivers.
const topServices = 5

// renderBody assembles the panel fragment: a live/stale status line, the
// current-period OCI infra-spend total, the top service breakdown, and the bounded
// spend trend. Every dynamic part — currency and service names are external
// usage-api data — is HTML-escaped in the helpers below, so a hostile or malformed
// usage-api response can never inject markup into the trusted panel HTML. Spend
// carries no OCI identifier, so none can appear here by construction.
func renderBody(spend *oci.Spend, stale bool, history []core.Signal) template.HTML {
	var sb strings.Builder
	sb.WriteString(statusLine(spend, stale))
	sb.WriteString(spendBlock(spend))
	sb.WriteString(spendTrend(history))
	return template.HTML(sb.String()) //nolint:gosec // every dynamic part escaped in the helpers
}

// statusLine reports whether the spend view is live or serving last-known data
// flagged stale. Its text is a fixed literal, never external data.
func statusLine(spend *oci.Spend, stale bool) string {
	if spend == nil {
		return `<p class="empty">no OCI spend mirrored yet — usage-api not reached</p>`
	}
	if stale {
		return `<p class="empty">STALE — OCI usage-api unreachable, showing last-known spend</p>`
	}
	return `<p>LIVE — infra spend from OCI usage-api (instance-principal, read-only)</p>`
}

// spendBlock renders the current-period total and the top per-service breakdown.
// The currency and each service name are external usage-api data and are
// HTML-escaped; the amounts and period are formatted numerically.
func spendBlock(spend *oci.Spend) string {
	if spend == nil {
		return ""
	}
	var sb strings.Builder
	total := fmt.Sprintf("%.2f %s", spend.Amount, spend.Currency)
	period := fmt.Sprintf("%s to %s (month-to-date)",
		spend.PeriodStart.Format("2006-01-02"), spend.PeriodEnd.Format("2006-01-02"))
	sb.WriteString("<h3>Current period</h3><ul>")
	sb.WriteString("<li>total " + template.HTMLEscapeString(total) + "</li>")
	sb.WriteString("<li>" + template.HTMLEscapeString(period) + "</li>")
	sb.WriteString("</ul>")
	sb.WriteString(serviceTable(spend))
	return sb.String()
}

// serviceTable renders the top-N per-service spend lines, each service name
// HTML-escaped as external usage-api data.
func serviceTable(spend *oci.Spend) string {
	if len(spend.Lines) == 0 {
		return `<p class="empty">no per-service spend</p>`
	}
	var sb strings.Builder
	sb.WriteString("<h3>Top services</h3><table><thead><tr><th>service</th><th>spend</th></tr></thead><tbody>")
	for i, line := range spend.Lines {
		if i >= topServices {
			break
		}
		amount := fmt.Sprintf("%.2f %s", line.Amount, spend.Currency)
		fmt.Fprintf(&sb, "<tr><td>%s</td><td>%s</td></tr>",
			template.HTMLEscapeString(line.Service), template.HTMLEscapeString(amount))
	}
	sb.WriteString("</tbody></table>")
	return sb.String()
}

// spendTrend renders the bounded recent-spend samples, each sample's text
// HTML-escaped since it is built from external usage-api figures.
func spendTrend(history []core.Signal) string {
	if len(history) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "<h3>Spend trend</h3><p>%d sample(s)</p><ul>", len(history))
	for _, s := range history {
		sb.WriteString("<li>" + template.HTMLEscapeString(s.Text) + "</li>")
	}
	sb.WriteString("</ul>")
	return sb.String()
}
