package cost

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"altune/overseer/internal/oci"
	"fmt"
	"html/template"
	"sort"
	"strings"
)

// topServices is how many per-service spend lines the panel lists. The spend is
// grouped by service and largest-first, so this is the top-N cost drivers.
const topServices = 5

// renderBody assembles the panel fragment: the OCI infra-spend half (status,
// current-period total, top service breakdown, spend trend) and the provider-usage
// half (status, per-provider call breakdown, usage trend), each flagged STALE
// independently. Every dynamic part — currency, service names, provider labels,
// trend text — is external source data and is HTML-escaped in the helpers below,
// so a hostile or malformed source response can never inject markup into the
// trusted panel HTML. Spend carries no OCI identifier and provider counts carry
// only bounded labels, so nothing sensitive appears here by construction.
func renderBody(spend *oci.Spend, spendStale bool, spendHistory []core.Signal, usage *goapi.ProviderUsage, usageStale bool, usageHistory []core.Signal) template.HTML {
	var sb strings.Builder
	sb.WriteString("<h2>Provider usage</h2>")
	sb.WriteString(usageStatusLine(usage, usageStale))
	sb.WriteString(usageBlock(usage))
	sb.WriteString(usageTrend(usageHistory))
	sb.WriteString("<h2>OCI infra spend</h2>")
	sb.WriteString(statusLine(spend, spendStale))
	sb.WriteString(spendBlock(spend))
	sb.WriteString(spendTrend(spendHistory))
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

// usageStatusLine reports whether the provider-usage view is live or serving
// last-known data flagged stale. Its text is a fixed literal, never external data.
func usageStatusLine(usage *goapi.ProviderUsage, stale bool) string {
	if usage == nil {
		return `<p class="empty">no provider usage mirrored yet — go-api not reached</p>`
	}
	if stale {
		return `<p class="empty">STALE — go-api unreachable, showing last-known provider usage</p>`
	}
	return `<p>LIVE — provider API usage mirrored from go-api /admin/metrics/live</p>`
}

// usageBlock renders the per-provider call breakdown, one row per provider with
// any activity, sorted by provider label for deterministic output. Each provider
// name is external go-api data and is HTML-escaped; the counts are formatted
// numerically. Map iteration order is randomized in Go, so sorting is what makes
// the panel stable across renders.
func usageBlock(usage *goapi.ProviderUsage) string {
	if usage == nil {
		return ""
	}
	names := make([]string, 0, len(*usage))
	var active int
	for name, o := range *usage {
		names = append(names, name)
		if o.Total() > 0 {
			active++
		}
	}
	if active == 0 {
		return `<p class="empty">no provider calls yet</p>`
	}
	sort.Strings(names)
	var sb strings.Builder
	sb.WriteString("<h3>Provider calls</h3><table><thead><tr><th>provider</th><th>ok</th><th>quota</th><th>error</th></tr></thead><tbody>")
	for _, name := range names {
		o := (*usage)[name]
		if o.Total() == 0 {
			continue
		}
		fmt.Fprintf(&sb, "<tr><td>%s</td><td>%d</td><td>%d</td><td>%d</td></tr>",
			template.HTMLEscapeString(name), o.OK, o.Quota, o.Error)
	}
	sb.WriteString("</tbody></table>")
	return sb.String()
}

// usageTrend renders the bounded recent provider-usage samples, each sample's text
// HTML-escaped since it is built from external go-api figures.
func usageTrend(history []core.Signal) string {
	if len(history) == 0 {
		return ""
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "<h3>Usage trend</h3><p>%d sample(s)</p><ul>", len(history))
	for _, s := range history {
		sb.WriteString("<li>" + template.HTMLEscapeString(s.Text) + "</li>")
	}
	sb.WriteString("</ul>")
	return sb.String()
}
