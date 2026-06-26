package handler

import (
	"net/http"
	"strings"

	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
)

// serveLogs returns a snapshot of recent log records, oldest first, optionally
// filtered to a minimum level via ?level=. Correlation grouping (by the corr_id
// attr) is done client-side in the console.
func (h *AdminHandler) serveLogs(w http.ResponseWriter, r *http.Request) {
	records := h.logRing.Snapshot()
	if min := r.URL.Query().Get("level"); min != "" {
		records = filterByLevel(records, min)
	}
	httputil.WriteJSON(w, http.StatusOK, records)
}

// streamLogs live-tails newly appended records over SSE until the operator
// disconnects.
func (h *AdminHandler) streamLogs(w http.ResponseWriter, r *http.Request) {
	ch, cancel := h.logRing.Subscribe()
	defer cancel()
	streamSSE(w, r, ch)
}

func filterByLevel(records []logging.CapturedRecord, min string) []logging.CapturedRecord {
	threshold := levelRank(min)
	out := make([]logging.CapturedRecord, 0, len(records))
	for _, rec := range records {
		if levelRank(rec.Level) >= threshold {
			out = append(out, rec)
		}
	}
	return out
}

// levelRank maps a slog level string (e.g. "INFO", "WARN+2") to an orderable
// rank. Unknown levels rank lowest so they are never filtered out by accident.
func levelRank(level string) int {
	switch {
	case strings.HasPrefix(strings.ToUpper(level), "ERROR"):
		return 3
	case strings.HasPrefix(strings.ToUpper(level), "WARN"):
		return 2
	case strings.HasPrefix(strings.ToUpper(level), "INFO"):
		return 1
	case strings.HasPrefix(strings.ToUpper(level), "DEBUG"):
		return 0
	default:
		return 0
	}
}
