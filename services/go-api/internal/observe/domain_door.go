package observe

import domain "altune/go-api/internal/observe/internal/core/domain"

type (
	Alert     = domain.Alert
	Condition = domain.Condition
	Severity  = domain.Severity
)

const (
	SeverityFix    = domain.SeverityFix
	SeverityLog    = domain.SeverityLog
	SeveritySignal = domain.SeveritySignal
)
