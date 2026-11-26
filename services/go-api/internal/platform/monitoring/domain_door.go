package monitoring

import domain "altune/go-api/internal/platform/monitoring/internal/core/domain"

type (
	Alert     = domain.Alert
	Condition = domain.Condition
	Severity  = domain.Severity
)

const (
	SeverityActNow = domain.SeverityActNow
	SeverityRecord = domain.SeverityRecord
	SeverityWatch  = domain.SeverityWatch
)
