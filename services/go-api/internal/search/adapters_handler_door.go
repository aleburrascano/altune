package search

import handler "altune/go-api/internal/search/adapters/handler"

type (
	DetailEnrichers   = handler.DetailEnrichers
	DiscoveryHandler  = handler.DiscoveryHandler
	DiscoveryServices = handler.DiscoveryServices
)

var NewDiscoveryHandler = handler.NewDiscoveryHandler

var DefaultAuthFailureLimit = handler.DefaultAuthFailureLimit
