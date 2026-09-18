package logs

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"fmt"
	"strings"
)

// filteredRecords decodes the bounded tail, keeps only records at or above
// minLevel, and normalizes each level to its canonical name. The records are
// watched-app data carried raw in the JSON payload; React escapes them on render,
// which is the escaping invariant moved off html/template onto the client.
func filteredRecords(records []core.Signal, minLevel string) []goapi.LogRecord {
	threshold := levelRank(minLevel)
	out := make([]goapi.LogRecord, 0, len(records))
	for _, s := range records {
		if levelRank(s.Kind) < threshold {
			continue
		}
		rec := decodeRecord(s)
		rec.Level = normalizeLevel(rec.Level)
		out = append(out, rec)
	}
	return out
}

// logsHealth grades the retained tail by what the watched app is actually saying,
// hoisting the panel's own colouring (web/src/panels/logs.panel.tsx): an ERROR
// line is red, a WARN line amber. It grades the tail the bucket is serving, so a
// source-down bucket still reports the errors it last saw rather than going green.
func logsHealth(records []goapi.LogRecord) (core.Severity, string) {
	errorLines, warnLines := countLevel(records, "ERROR"), countLevel(records, "WARN")
	headline := fmt.Sprintf("%d errors · %d warnings · %d lines", errorLines, warnLines, len(records))
	switch {
	case errorLines > 0:
		return core.SeverityCritical, headline
	case warnLines > 0:
		return core.SeverityWarn, headline
	default:
		return core.SeverityOK, headline
	}
}

// countLevel counts tail records at the given canonical level, normalizing each
// so an odd casing or a "warning" spelling is still counted.
func countLevel(records []goapi.LogRecord, level string) int {
	n := 0
	for _, rec := range records {
		if normalizeLevel(rec.Level) == level {
			n++
		}
	}
	return n
}

// effectiveLevel names the active minimum level so the frontend can show the
// tail's filtering. Everything at or below DEBUG collapses to "ALL".
func effectiveLevel(minLevel string) string {
	if levelRank(minLevel) <= levelRank("DEBUG") {
		return "ALL"
	}
	return normalizeLevel(minLevel)
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
// other value is passed through trimmed.
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
