package handler

import (
	"net/http"

	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/shared/httputil"
)

func (h *AdminHandler) serveEval(w http.ResponseWriter, _ *http.Request) {
	if h.evalMeter == nil {
		httputil.WriteJSON(w, http.StatusOK, evalmeter.Status{Enabled: false, State: "disabled"})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.evalMeter.Status())
}
