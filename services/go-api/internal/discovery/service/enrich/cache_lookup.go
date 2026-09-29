package enrich

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"fmt"
)

var ErrDegraded = errors.New("enrichment degraded")

func degraded(err error) error {
	if err == nil || errors.Is(err, ErrDegraded) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrDegraded, err)
}

func CachedLookup[T any](
	ctx context.Context,
	cache ports.NameKeyedCache[T],
	nameKey string,
	empty T,
	fetch func(context.Context) (T, bool, error),
) (T, error) {
	if nameKey == "" {
		cache = nil
	}

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
		return empty, degraded(err)
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

func kindNameKey(kind domain.ResultKind, artist, title string) string {
	nameKey := textnorm.NameKey(artist, title)
	if nameKey == "" {
		return ""
	}
	return kind.String() + textnorm.KeySeparator + nameKey
}
