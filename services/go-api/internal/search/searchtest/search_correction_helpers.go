package searchtest

import "altune/go-api/internal/search/internal/adapters/memory"

type QueryFakeProvider = memory.QueryFakeProvider

var (
	CaptureLogs = memory.CaptureLogs
	LoggedEvent = memory.LoggedEvent
)
