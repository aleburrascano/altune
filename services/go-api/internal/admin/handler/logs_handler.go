package handler

import (
	"net/http"
	"strings"

	"altune/go-api/internal/shared/httputil"
	"altune/go-api/internal/shared/logging"
)

func (h *AdminHandler) serveLogs(w http.ResponseWriter, r *http.Request) {
	records := h.logRing.Snapshot()
	if min := r.URL.Query().Get("level"); min != "" {
		records = filterByLevel(records, min)
	}
	httputil.WriteJSON(w, http.StatusOK, records)
}

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
