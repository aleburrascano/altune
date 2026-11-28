package search

import (
	handler "altune/go-api/internal/search/internal/adapters/handler"
	"altune/go-api/internal/search/internal/service"
)

type (
	ClientEventSink = handler.ClientEventSink
	NoActivityFeed  = service.NoopActivityFeed
	NoClientEvents  = handler.NoClientEvents
)
