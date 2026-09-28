package logs

import (
	"altune/overseer/internal/core"
	"altune/overseer/internal/goapi"
	"fmt"
	"strings"
)

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

func countLevel(records []goapi.LogRecord, level string) int {
	n := 0
	for _, rec := range records {
		if normalizeLevel(rec.Level) == level {
			n++
		}
	}
	return n
}

func effectiveLevel(minLevel string) string {
	if levelRank(minLevel) <= levelRank("DEBUG") {
		return "ALL"
	}
	return normalizeLevel(minLevel)
}

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
