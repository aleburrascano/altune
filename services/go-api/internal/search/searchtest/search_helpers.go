package searchtest

import "altune/go-api/internal/search/internal/adapters/memory"

type (
	FakeProvider        = memory.FakeProvider
	PanickingEventStore = memory.PanickingEventStore
)

var (
	NewQuery = memory.NewQuery
	NewUser  = memory.NewUser
)
