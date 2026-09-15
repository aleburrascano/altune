package reliability

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"fmt"
	"html/template"
	"strings"
)

// renderBody assembles the panel fragment: the authoritative reachability line
// from the own poll, the mirrored dependency pills (flagged stale when the admin
// read is down), and the bounded health history. Every dynamic part — all of it
// watched-app data — is HTML-escaped in the helpers below, so a hostile go-api
// response can never inject markup into the trusted panel HTML.
func renderBody(reach goapi.Status, last *goapi.OperatorHealth, stale bool, history []core.Signal) template.HTML {
	var sb strings.Builder
	sb.WriteString(reachabilityLine(reach))
	sb.WriteString(pills(last, stale))
	sb.WriteString(historyList(history))
	return template.HTML(sb.String()) //nolint:gosec // every dynamic part escaped in the helpers
}

// reachabilityLine renders the own-poll signal — the authoritative "is the app
// up". Its text is a fixed literal keyed on the poll status, never watched-app
// data.
func reachabilityLine(reach goapi.Status) string {
	switch reach {
	case goapi.StatusUp:
		return `<p>REACHABLE — own poll reports go-api up</p>`
	case goapi.StatusDown:
		return `<p class="down">UNREACHABLE — own poll reports go-api down</p>`
	default:
		return `<p class="empty">reachability unknown — first poll pending</p>`
	}
}

// pills renders the mirrored DB/Redis/Auth dependency pills from the last-known
// operator health. When the admin read is currently unreachable it flags the
// block STALE while still showing the last-known pills — degrade, don't go dark.
func pills(last *goapi.OperatorHealth, stale bool) string {
	if last == nil {
		return `<p class="empty">no dependency health mirrored yet</p>`
	}
	var sb strings.Builder
	if stale {
		sb.WriteString(`<p class="empty">STALE — admin health unreachable, showing last-known dependency health</p>`)
	} else {
		sb.WriteString(`<p>Dependency health (mirrored from /admin/health)</p>`)
	}
	sb.WriteString("<ul>")
	sb.WriteString(pill("DB", last.DB, last.Detail.DBError))
	sb.WriteString(pill("Redis", last.Redis, last.Detail.RedisError))
	sb.WriteString(pill("Auth", last.Auth, last.Detail.AuthError))
	sb.WriteString("</ul>")
	return sb.String()
}

// pill renders one dependency pill. The status and error text are watched-app
// data and are HTML-escaped; the name is escaped too for uniformity so no future
// caller can leak markup through it.
func pill(name, status, errText string) string {
	var sb strings.Builder
	sb.WriteString("<li>")
	sb.WriteString(template.HTMLEscapeString(name) + ": " + template.HTMLEscapeString(status))
	if errText != "" {
		sb.WriteString(" — " + template.HTMLEscapeString(errText))
	}
	sb.WriteString("</li>")
	return sb.String()
}

// historyList renders the bounded dependency-health history, each sample's text
// HTML-escaped since it is built from watched-app data.
func historyList(history []core.Signal) string {
	if len(history) == 0 {
		return `<p class="empty">no health samples yet</p>`
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "<p>%d health sample(s)</p><ul>", len(history))
	for _, s := range history {
		sb.WriteString("<li>" + template.HTMLEscapeString(s.Text) + "</li>")
	}
	sb.WriteString("</ul>")
	return sb.String()
}
