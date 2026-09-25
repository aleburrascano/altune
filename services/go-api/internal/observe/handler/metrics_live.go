package handler

import (
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

func (h *Handler) serveMetricsLive(w http.ResponseWriter, _ *http.Request) {
	if h.deps.LiveMetrics == nil {
		httputil.WriteJSON(w, http.StatusOK, struct{}{})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.deps.LiveMetrics())
}
