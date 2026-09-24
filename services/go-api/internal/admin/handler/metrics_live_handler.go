package handler

import (
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

type LiveMetricsSource func() any

func (h *AdminHandler) WithLiveMetrics(s LiveMetricsSource) *AdminHandler {
	h.liveMetrics = s
	return h
}

func (h *AdminHandler) serveMetricsLive(w http.ResponseWriter, _ *http.Request) {
	if h.liveMetrics == nil {
		httputil.WriteJSON(w, http.StatusOK, struct{}{})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.liveMetrics())
}
