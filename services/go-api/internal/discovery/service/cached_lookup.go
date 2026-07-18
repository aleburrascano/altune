package service

import (
	"context"

	"altune/go-api/internal/discovery/ports"
)

// CachedLookup forwards to ports.CachedLookup; canonical home is ports.
func CachedLookup[T any](
	ctx context.Context,
	cache ports.NameKeyedCache[T],
	nameKey string,
	empty T,
	fetch func(context.Context) (T, bool, error),
) (T, error) {
	return ports.CachedLookup(ctx, cache, nameKey, empty, fetch)
}
