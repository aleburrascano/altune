package discovery

import handler "altune/go-api/internal/discovery/adapters/handler"

type (
	DetailEnrichers   = handler.DetailEnrichers
	DiscoveryHandler  = handler.DiscoveryHandler
	DiscoveryServices = handler.DiscoveryServices
)

var NewDiscoveryHandler = handler.NewDiscoveryHandler

var DefaultAuthFailureLimit = handler.DefaultAuthFailureLimit
