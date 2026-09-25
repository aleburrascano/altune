package goapi

import (
	"errors"
	"fmt"
	"time"
)

const unclassifiedRefreshFailure = "unclassified"

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

func (s *RefreshingTokenSource) Health() CredentialHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	return CredentialHealth{
		LastRefresh:         s.refreshedAt,
		ConsecutiveFailures: s.failCount,
		PersistFailed:       s.persistFailed,
		PasswordGrant:       s.signIn != nil,
		LastError:           refreshFailureClass(s.lastErr),
	}
}

func refreshFailureClass(err error) string {
	if err == nil {
		return ""
	}
	var refreshErr *TokenRefreshError
	if !errors.As(err, &refreshErr) {
		return unclassifiedRefreshFailure
	}
	if refreshErr.Status == 0 {
		return refreshErr.Stage
	}
	if refreshErr.Stage == "status" {
		return fmt.Sprintf("status %d", refreshErr.Status)
	}
	return fmt.Sprintf("%s status %d", refreshErr.Stage, refreshErr.Status)
}
