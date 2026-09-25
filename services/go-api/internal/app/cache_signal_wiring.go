package app

import (
	"expvar"
	"time"

	discoveryCacheAdapters "altune/go-api/internal/discovery/adapters/cache"
)

const cacheOpsVarName = "discovery_cache_ops"

func cacheSignal() *discoveryCacheAdapters.Signal {
	ops, ok := expvar.Get(cacheOpsVarName).(*expvar.Map)
	if !ok {
		ops = expvar.NewMap(cacheOpsVarName)
	}
	return discoveryCacheAdapters.NewSignal(ops, time.Now)
}

func cacheSignalOption() discoveryCacheAdapters.Option {
	return discoveryCacheAdapters.WithSignal(cacheSignal())
}
