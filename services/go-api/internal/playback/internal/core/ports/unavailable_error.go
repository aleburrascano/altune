package ports

import (
	apierror "altune/go-api/internal/shared/apierror"
	"time"
)

var ErrQueueStateUnavailable error = apierror.NewError(apierror.CodePlaybackUnavailable, "queue state temporarily unavailable").WithRetryAfter(time.Second)
