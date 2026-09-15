package handler

import (
	"altune/go-api/internal/admin/alert"
	"altune/go-api/internal/admin/evalmeter"
	"altune/go-api/internal/shared/httputil"
	"log/slog"
	"net/http"
)

// The kill-switch routes flip the in-memory runloop pause gate on the alert
// monitor and eval meter so an operator can silence a misbehaving loop without
// a redeploy. The gate is per process and resets on restart; admin auth is a
// bearer token (no cookies), so these POSTs need no CSRF token, matching the
// other state-changing admin routes.

var (
	errAlertMonitorUnavailable = &codedError{
		msg:    "alert monitor not configured",
		status: http.StatusServiceUnavailable,
		code:   "admin.alert_monitor_unavailable",
	}
	errEvalMeterUnavailable = &codedError{
		msg:    "eval meter not configured",
		status: http.StatusServiceUnavailable,
		code:   "admin.eval_meter_unavailable",
	}
)

type alertStatusDTO struct {
	Enabled bool `json:"enabled"`
	Paused  bool `json:"paused"`
}

func (h *AdminHandler) alertStatus() alertStatusDTO {
	if h.alertMonitor == nil {
		return alertStatusDTO{}
	}
	return alertStatusDTO{Enabled: true, Paused: h.alertMonitor.Paused()}
}

func (h *AdminHandler) serveAlerts(w http.ResponseWriter, _ *http.Request) {
	httputil.WriteJSON(w, http.StatusOK, h.alertStatus())
}

func (h *AdminHandler) pauseAlerts(w http.ResponseWriter, r *http.Request) {
	h.flipAlerts(w, r, (*alert.Monitor).Pause)
}

func (h *AdminHandler) resumeAlerts(w http.ResponseWriter, r *http.Request) {
	h.flipAlerts(w, r, (*alert.Monitor).Resume)
}

// flipAlerts applies one kill-switch transition to the alert monitor.
func (h *AdminHandler) flipAlerts(w http.ResponseWriter, r *http.Request, flip func(*alert.Monitor)) {
	if h.alertMonitor == nil {
		httputil.HandleServiceError(w, r, errAlertMonitorUnavailable)
		return
	}
	flip(h.alertMonitor)
	st := h.alertStatus()
	slog.InfoContext(r.Context(), "admin.kill_switch", "loop", "alert_monitor", "paused", st.Paused)
	httputil.WriteJSON(w, http.StatusOK, st)
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
	slog.InfoContext(r.Context(), "admin.kill_switch", "loop", "eval_meter", "paused", st.Paused)
	httputil.WriteJSON(w, http.StatusOK, st)
}
