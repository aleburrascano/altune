package discoverytest

import "altune/go-api/internal/discovery/internal/adapters/memory"

type (
	FakeProvider        = memory.FakeProvider
	PanickingEventStore = memory.PanickingEventStore
)

var (
	NewQuery = memory.NewQuery
	NewUser  = memory.NewUser
)
