package discoverytest

import "altune/go-api/internal/discovery/internal/adapters/memory"

type (
	CountingProvider = memory.CountingProvider
	FakeResultCache  = memory.FakeResultCache
)

var NewFakeResultCache = memory.NewFakeResultCache
