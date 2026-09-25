package handler

import (
	"altune/go-api/internal/observe/evalmeter"
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

var errEvalMeterUnavailable = &codedError{
	msg:    "eval meter not configured",
	status: http.StatusServiceUnavailable,
	code:   "admin.eval_meter_unavailable",
}

func (h *AdminHandler) serveEval(w http.ResponseWriter, _ *http.Request) {
	if h.evalMeter == nil {
		httputil.WriteJSON(w, http.StatusOK, evalmeter.Status{Enabled: false, State: evalmeter.StateDisabled})
		return
	}
	httputil.WriteJSON(w, http.StatusOK, h.evalMeter.Status())
}

func (h *AdminHandler) pauseEval(w http.ResponseWriter, r *http.Request) {
	h.flipEval(w, r, (*evalmeter.Meter).Pause)
}

func (h *AdminHandler) resumeEval(w http.ResponseWriter, r *http.Request) {
	h.flipEval(w, r, (*evalmeter.Meter).Resume)
}

// flipEval applies one kill-switch transition to the eval meter.
func (h *AdminHandler) flipEval(w http.ResponseWriter, r *http.Request, flip func(*evalmeter.Meter)) {
	if h.evalMeter == nil {
		httputil.HandleServiceError(w, r, errEvalMeterUnavailable)
		return
	}
	flip(h.evalMeter)
	st := h.evalMeter.Status()
	auditKillSwitch(r.Context(), "eval_meter", st.Paused)
	httputil.WriteJSON(w, http.StatusOK, st)
}
