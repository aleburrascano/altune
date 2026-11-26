package search

import (
	handler "altune/go-api/internal/search/internal/adapters/handler"
	useractivity "altune/go-api/internal/search/internal/service/useractivity"
)

type (
	ClientEventSink        = handler.ClientEventSink
	Enricher               = handler.Enricher
	NoActivityFeed         = useractivity.NoopActivityFeed
	NoClientEvents         = handler.NoClientEvents
	NoOwnership            = handler.NoOwnership
	NoUnreleased           = handler.NoUnreleased
	OwnershipStamper       = handler.OwnershipStamper
	UnconfiguredEnrichment = handler.UnconfiguredEnrichment
)
