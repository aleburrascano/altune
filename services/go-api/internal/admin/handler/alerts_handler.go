package handler

import (
	"altune/go-api/internal/admin/alert"
	"altune/go-api/internal/shared/httputil"
	"net/http"
)

var errAlertMonitorUnavailable = &codedError{
	msg:    "alert monitor not configured",
	status: http.StatusServiceUnavailable,
	code:   "admin.alert_monitor_unavailable",
}

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
	auditKillSwitch(r.Context(), "alert_monitor", st.Paused)
	httputil.WriteJSON(w, http.StatusOK, st)
}
