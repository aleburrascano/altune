package logs

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
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
