package ports

import (
	shareddomain "altune/go-api/internal/shared/domain"
	"time"
)

var ErrQueueStateUnavailable error = shareddomain.NewError(shareddomain.CodePlaybackUnavailable, "queue state temporarily unavailable").WithRetryAfter(time.Second)
