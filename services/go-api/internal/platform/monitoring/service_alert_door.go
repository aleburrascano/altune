package monitoring

import alert "altune/go-api/internal/platform/monitoring/internal/shell/alert"

type AlertMonitor = alert.Monitor

var NewAlertMonitor = alert.NewMonitor

var ErrInvalidInterval = alert.ErrInvalidInterval
