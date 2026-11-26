package search

import related "altune/go-api/internal/search/service/related"

var (
	NewFindRelatedService      = related.NewFindRelatedService
	NewGetRelatedTracksService = related.NewGetRelatedTracksService
	WithRelatedCircuitBreaker  = related.WithRelatedCircuitBreaker
)
