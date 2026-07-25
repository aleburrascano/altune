package enrich

import (
	"context"

	"altune/go-api/internal/discovery/ports"
)

func CachedLookup[T any](
	ctx context.Context,
	cache ports.NameKeyedCache[T],
	nameKey string,
	empty T,
	fetch func(context.Context) (T, bool, error),
) (T, error) {
	if cache != nil {
		if cached, found, _ := cache.Get(ctx, nameKey); found {
			return cached, nil
		}
		if negative, _ := cache.GetNegative(ctx, nameKey); negative {
			return empty, nil
		}
	}

	value, found, err := fetch(ctx)
	if err != nil {
		return empty, nil
	}
	if !found {
		if cache != nil {
			_ = cache.SetNegative(ctx, nameKey)
		}
		return empty, nil
	}

	if cache != nil {
		_ = cache.Set(ctx, nameKey, value)
	}
	return value, nil
}
