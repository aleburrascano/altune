package handler

import (
	"net/http"

	"altune/go-api/internal/shared/httputil"
)

// serveEventRates returns per-type event counts over the rolling window.
func (h *AdminHandler) serveEventRates(w http.ResponseWriter, _ *http.Request) {
	if h.eventFeed == nil {
		httputil.WriteJSON(w, http.StatusOK, map[string]int{})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.eventFeed.Rates())
}

// streamEvents live-streams redacted (type + time) events to the operator.
func (h *AdminHandler) streamEvents(w http.ResponseWriter, r *http.Request) {
	if h.eventFeed == nil {
		httputil.InternalError(w, "event feed unavailable")
		return
	}
	ch, cancel := h.eventFeed.Subscribe()
	defer cancel()
	streamSSE(w, r, ch)
}
