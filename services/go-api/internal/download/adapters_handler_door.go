package download

import handler "altune/go-api/internal/download/internal/adapters/handler"

type (
	ReacquireHandler = handler.ReacquireHandler
	RetryHandler     = handler.RetryHandler
)

var (
	NewReacquireHandler = handler.NewReacquireHandler
	NewRetryHandler     = handler.NewRetryHandler
)
