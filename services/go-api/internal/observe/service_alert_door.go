package observe

import alert "altune/go-api/internal/observe/service/alert"

type AlertMonitor = alert.Monitor

var NewAlertMonitor = alert.NewMonitor
