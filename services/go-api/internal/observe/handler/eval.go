package handler

import (
	"altune/go-api/internal/observe/evalmeter"
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

func (h *Handler) serveEval(w http.ResponseWriter, _ *http.Request) {
	if h.deps.Eval == nil {
		httputil.WriteJSON(w, http.StatusOK, evalmeter.Status{Enabled: false, State: evalmeter.StateDisabled})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.deps.Eval.Status())
}
