package logs

import (
	"altune/overseer/internal/core"
	"fmt"
	"html/template"
	"sort"
	"strings"
)

// renderBody builds the Logs panel HTML from a tail snapshot. It filters the tail
// to records at or above minLevel, then renders each. Every dynamic part — the
// level, the message, and every field key and value — is HTML-escaped here (log
// text is watched-app data; a hostile log line must never inject markup into the
// trusted panel), so the assembled string is safe to mark as template.HTML.
func renderBody(records []core.Signal, minLevel string, stale bool) template.HTML {
	var sb strings.Builder
	sb.WriteString(statusLine(stale))
	filtered := filterByLevel(records, minLevel)
	sb.WriteString(levelLine(minLevel))
	sb.WriteString(tail(filtered))
	return template.HTML(sb.String()) //nolint:gosec // every dynamic part escaped below
}

func statusLine(stale bool) string {
	if stale {
		return `<p class="empty">STALE — go-api unreachable, showing last-known logs</p>`
	}
	return `<p>LIVE — streaming go-api logs</p>`
}

// levelLine names the active minimum level so the tail's filtering is visible
// rather than silent. The label is one of the fixed known levels, but it is
// HTML-escaped anyway — the panel never trusts a string it renders.
func levelLine(minLevel string) string {
	label := normalizeLevel(minLevel)
	if levelRank(minLevel) <= levelRank("DEBUG") {
		label = "ALL"
	}
	return fmt.Sprintf(`<p>Level ≥ %s</p>`, template.HTMLEscapeString(label))
}

// tail renders the filtered records, newest last (Snapshot is oldest-first). An
// empty tail renders a gap message rather than a blank block, so "nothing yet" is
// visible and distinct from "source down".
func tail(records []core.Signal) string {
	if len(records) == 0 {
		return `<p class="empty">no logs yet</p>`
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "<p>%d line(s)</p><ul>", len(records))
	for _, s := range records {
		sb.WriteString(line(s))
	}
	sb.WriteString("</ul>")
	return sb.String()
}

// line renders one log record: level, message and every field, each individually
// HTML-escaped. A hostile message or a hostile field key/value can therefore
// never break out of its text node into the surrounding markup.
func line(s core.Signal) string {
	rec := decodeRecord(s)
	var sb strings.Builder
	sb.WriteString("<li>")
	fmt.Fprintf(&sb, `<span class="level">%s</span> `, template.HTMLEscapeString(normalizeLevel(rec.Level)))
	sb.WriteString(template.HTMLEscapeString(rec.Message))
	if len(rec.Fields) > 0 {
		sb.WriteString(fields(rec.Fields))
	}
	sb.WriteString("</li>")
	return sb.String()
}

// fields renders a record's attribute bag in a stable key order, escaping both
// the key and the value of every field. Deterministic ordering makes the panel
// stable and the render testable.
func fields(f map[string]string) string {
	keys := make([]string, 0, len(f))
	for k := range f {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString(" <em>")
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(" ")
		}
		fmt.Fprintf(&sb, "%s=%s", template.HTMLEscapeString(k), template.HTMLEscapeString(f[k]))
	}
	sb.WriteString("</em>")
	return sb.String()
}

// filterByLevel keeps only records whose level is at or above min. It mirrors
// go-api's own level ranking (internal/admin/handler/logs_handler.go), so the
// Overseer tail and the source agree on what "≥ WARN" means. An empty or
// unrecognized min shows everything (DEBUG and up).
func filterByLevel(records []core.Signal, minimum string) []core.Signal {
	threshold := levelRank(minimum)
	out := make([]core.Signal, 0, len(records))
	for _, s := range records {
		if levelRank(s.Kind) >= threshold {
			out = append(out, s)
		}
	}
	return out
}

// levelRank maps a level string to a total order, mirroring go-api's ranking so
// the two never disagree. Unknown levels rank as DEBUG (the floor), so a filter
// never silently hides an unclassifiable line above the floor.
func levelRank(level string) int {
	switch {
	case strings.HasPrefix(strings.ToUpper(level), "ERROR"):
		return 3
	case strings.HasPrefix(strings.ToUpper(level), "WARN"):
		return 2
	case strings.HasPrefix(strings.ToUpper(level), "INFO"):
		return 1
	default:
		return 0
	}
}

// normalizeLevel renders a level for display: known levels are upper-cased to
// their canonical name, an empty level becomes INFO (slog's default), and any
// other value is passed through trimmed (it is still HTML-escaped by the caller).
func normalizeLevel(level string) string {
	trimmed := strings.TrimSpace(level)
	if trimmed == "" {
		return "INFO"
	}
	switch {
	case strings.HasPrefix(strings.ToUpper(trimmed), "ERROR"):
		return "ERROR"
	case strings.HasPrefix(strings.ToUpper(trimmed), "WARN"):
		return "WARN"
	case strings.HasPrefix(strings.ToUpper(trimmed), "INFO"):
		return "INFO"
	case strings.HasPrefix(strings.ToUpper(trimmed), "DEBUG"):
		return "DEBUG"
	default:
		return trimmed
	}
}
