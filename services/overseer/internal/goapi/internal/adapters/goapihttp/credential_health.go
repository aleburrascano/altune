package goapihttp

import (
	"altune/overseer/internal/goapi/internal/core"
	"errors"
	"fmt"
)

const unclassifiedRefreshFailure = "unclassified"

func (s *RefreshingTokenSource) Health() core.CredentialHealth {
	s.mu.Lock()
	defer s.mu.Unlock()
	return core.CredentialHealth{
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
