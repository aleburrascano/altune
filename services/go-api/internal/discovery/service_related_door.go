package discovery

import related "altune/go-api/internal/discovery/service/related"

type GetRelatedTracksService = related.GetRelatedTracksService

var (
	NewFindRelatedService         = related.NewFindRelatedService
	NewGetRelatedTracksService    = related.NewGetRelatedTracksService
	WithFindRelatedCircuitBreaker = related.WithFindRelatedCircuitBreaker
	WithRelatedCircuitBreaker     = related.WithRelatedCircuitBreaker
)
