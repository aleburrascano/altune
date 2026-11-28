package handler

import (
	"altune/go-api/internal/platform/eventstream/internal/domain"
	"errors"
)

func connLimitLogEvent(err error) string {
	if errors.Is(err, domain.ErrGlobalConnLimit) {
		return "sse.global_connection_limit"
	}
	return "sse.connection_limit"
}
