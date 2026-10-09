package database

import "strings"

const (
	sqlStateAdminShutdown        = "57P01"
	sqlStateCrashShutdown        = "57P02"
	sqlStateCannotConnectNow     = "57P03"
	sqlStateSerializationFailure = "40001"
	sqlStateDeadlockDetected     = "40P01"
	sqlStateClassConnection      = "08"
	sqlStateClassInsufficientRes = "53"
)

func IsTransientSQLState(code string) bool {
	switch code {
	case sqlStateAdminShutdown, sqlStateCrashShutdown, sqlStateCannotConnectNow,
		sqlStateSerializationFailure, sqlStateDeadlockDetected:
		return true
	}
	return strings.HasPrefix(code, sqlStateClassConnection) || strings.HasPrefix(code, sqlStateClassInsufficientRes)
}
