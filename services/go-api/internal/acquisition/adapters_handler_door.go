package acquisition

import handler "altune/go-api/internal/acquisition/internal/adapters/handler"

type (
	ReacquireHandler = handler.ReacquireHandler
	RetryHandler     = handler.RetryHandler
)

var (
	NewReacquireHandler = handler.NewReacquireHandler
	NewRetryHandler     = handler.NewRetryHandler
)
