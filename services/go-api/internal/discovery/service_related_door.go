package discovery

import related "altune/go-api/internal/discovery/service/related"

var (
	NewFindRelatedService         = related.NewFindRelatedService
	NewGetRelatedTracksService    = related.NewGetRelatedTracksService
	WithFindRelatedCircuitBreaker = related.WithFindRelatedCircuitBreaker
	WithRelatedCircuitBreaker     = related.WithRelatedCircuitBreaker
)
