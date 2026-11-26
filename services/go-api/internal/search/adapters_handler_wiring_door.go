package search

import (
	handler "altune/go-api/internal/search/internal/adapters/handler"
	useractivity "altune/go-api/internal/search/internal/service/useractivity"
)

type (
	ClientEventSink = handler.ClientEventSink
	NoActivityFeed  = useractivity.NoopActivityFeed
	NoClientEvents  = handler.NoClientEvents
)
