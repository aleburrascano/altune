package search

import handler "altune/go-api/internal/search/adapters/handler"

type (
	DetailEnrichers   = handler.DetailEnrichers
	DiscoveryHandler  = handler.DiscoveryHandler
	DiscoveryServices = handler.DiscoveryServices
)

var (
	ErrDiscoveryClockRequired   = handler.ErrClockRequired
	ErrDiscoveryServiceRequired = handler.ErrServiceRequired
	ErrOwnershipStamperRequired = handler.ErrOwnershipRequired
	MustNewDiscoveryHandler     = handler.MustNewDiscoveryHandler
	NewDiscoveryHandler         = handler.NewDiscoveryHandler
)

var DefaultAuthFailureLimit = handler.DefaultAuthFailureLimit
