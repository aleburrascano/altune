package discovery

import related "altune/go-api/internal/discovery/service/related"

var (
	NewFindRelatedService      = related.NewFindRelatedService
	NewGetRelatedTracksService = related.NewGetRelatedTracksService
	WithRelatedCircuitBreaker  = related.WithRelatedCircuitBreaker
)
