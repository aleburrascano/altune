package searchtest

import "altune/go-api/internal/search/internal/adapters/memory"

type (
	CountingProvider = memory.CountingProvider
	FakeResultCache  = memory.FakeResultCache
)

var NewFakeResultCache = memory.NewFakeResultCache
