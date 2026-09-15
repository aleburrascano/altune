package usage

import (
	"fmt"
	"html/template"
	"strings"
)

// renderBody builds the Usage panel HTML from a rollup snapshot. Every dynamic
// part is HTML-escaped here (search queries are watched-app text; play kinds and
// window labels come from go-api but are escaped too — the panel never trusts the
// stream), so the assembled string is safe to mark as template.HTML.
func renderBody(v view, stale bool) template.HTML {
	var sb strings.Builder
	sb.WriteString(statusLine(stale))
	sb.WriteString(section("Top searches", "no searches yet", v.searches))
	sb.WriteString(section("Plays by kind", "no plays yet", v.plays))
	sb.WriteString(section("Activity timeline", "no activity yet", v.timeline))
	return template.HTML(sb.String()) //nolint:gosec // every dynamic part escaped below
}

func statusLine(stale bool) string {
	if stale {
		return `<p class="empty">STALE — go-api unreachable, showing last-known usage</p>`
	}
	return `<p>LIVE — aggregating go-api usage events</p>`
}

// section renders one labelled rollup list. An empty rollup renders the given
// gap message rather than a blank block, so a missing dimension is visible.
func section(title, empty string, entries []entry) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "<h3>%s</h3>", template.HTMLEscapeString(title))
	if len(entries) == 0 {
		fmt.Fprintf(&sb, `<p class="empty">%s</p>`, template.HTMLEscapeString(empty))
		return sb.String()
	}
	sb.WriteString("<ul>")
	for _, e := range entries {
		// Key is watched-app data (a search query, an event kind, a window label);
		// escape it so a hostile payload can never inject markup into the panel.
		fmt.Fprintf(&sb, "<li>%s — %d</li>", template.HTMLEscapeString(e.Key), e.Count)
	}
	sb.WriteString("</ul>")
	return sb.String()
}
