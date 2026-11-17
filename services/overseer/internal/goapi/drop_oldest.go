package goapi

import (
	goapihttp "altune/overseer/internal/goapi/internal/adapters/goapihttp"
	"math"
)

func DrainPending[T any](src any, pending <-chan T) []T {
	return goapihttp.DrainPending(src, pending)
}

type dropSource interface {
	Dropped() int
}

func TotalDropped(src any, evicted int) int {
	streamDrops := 0
	if ds, ok := src.(dropSource); ok {
		streamDrops = ds.Dropped()
	}
	if streamDrops > math.MaxInt-evicted {
		return math.MaxInt
	}
	return evicted + streamDrops
}
