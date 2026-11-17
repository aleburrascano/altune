package core

import "time"

type CredentialHealth struct {
	LastRefresh         time.Time
	ConsecutiveFailures int
	PersistFailed       bool
	PasswordGrant       bool
	LastError           string
}

func (h CredentialHealth) OK() bool {
	hasRefreshed := !h.LastRefresh.IsZero()
	return hasRefreshed && h.ConsecutiveFailures == 0
}
