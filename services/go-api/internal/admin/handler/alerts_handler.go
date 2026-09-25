package handler

import (
	"altune/go-api/internal/admin/alert"
	"altune/go-api/internal/shared/httputil"
	"net/http"
	"time"
)

var errAlertMonitorUnavailable = &codedError{
	msg:    "alert monitor not configured",
	status: http.StatusServiceUnavailable,
	code:   "admin.alert_monitor_unavailable",
}

type alertStatusDTO struct {
	Enabled bool `json:"enabled"`
	Paused  bool `json:"paused"`

	PushConfigured    bool       `json:"push_configured"`
	LastNotifyOKAt    *time.Time `json:"last_notify_ok_at,omitempty"`
	LastNotifyErrorAt *time.Time `json:"last_notify_error_at,omitempty"`

	LastPassAt                *time.Time `json:"last_pass_at,omitempty"`
	LastNotifyOK              bool       `json:"last_notify_ok"`
	ConsecutiveNotifyFailures int64      `json:"consecutive_notify_failures"`
	ContainedPanics           uint64     `json:"contained_panics"`
	NotifierNop               bool       `json:"notifier_nop"`
}

func (h *AdminHandler) alertStatus() alertStatusDTO {
	if h.alertMonitor == nil {
		return alertStatusDTO{}
	}
	st := h.alertMonitor.Status()
	dto := alertStatusDTO{
		Enabled:                   true,
		Paused:                    h.alertMonitor.Paused(),
		LastNotifyOK:              st.LastNotifyOK,
		ConsecutiveNotifyFailures: st.ConsecutiveFailures,
		ContainedPanics:           st.ContainedPanics,
		NotifierNop:               st.NopNotifier,
		PushConfigured:            !st.NopNotifier,
	}
	if !st.LastNotifyOKAt.IsZero() {
		dto.LastNotifyOKAt = &st.LastNotifyOKAt
	}
	if !st.LastNotifyErrorAt.IsZero() {
		dto.LastNotifyErrorAt = &st.LastNotifyErrorAt
	}
	if !st.LastPass.IsZero() {
		dto.LastPassAt = &st.LastPass
	}
	return dto
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
