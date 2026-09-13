package enrich

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
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

// kindNameKey builds the normalized "kind + artist + title" cache key shared by
// the Deezer, Last.fm and lyrics enrichment name-keyed caches.
func kindNameKey(kind domain.ResultKind, artist, title string) string {
	return textnorm.NormalizeForMatch(kind.String() + " " + artist + " " + title)
}
