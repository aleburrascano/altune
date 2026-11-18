package discoverytest

import "altune/go-api/internal/discovery/internal/adapters/memory"

type QueryFakeProvider = memory.QueryFakeProvider

var (
	CaptureLogs = memory.CaptureLogs
	LoggedEvent = memory.LoggedEvent
)
