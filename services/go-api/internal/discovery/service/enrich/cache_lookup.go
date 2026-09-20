package enrich

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared/textnorm"
	"context"
	"errors"
	"fmt"
)

// ErrDegraded marks an enrichment result that is empty because the upstream
// fetch failed (timeout, 429, 5xx), as opposed to the provider genuinely having
// no data. The empty value returned alongside it is safe to render; callers
// should surface the distinction (and must not treat it as a permanent miss).
var ErrDegraded = errors.New("enrichment degraded")

func degraded(err error) error {
	if err == nil || errors.Is(err, ErrDegraded) {
		return err
	}
	return fmt.Errorf("%w: %w", ErrDegraded, err)
}

// CachedLookup serves a name-keyed enrichment from cache, falling back to fetch.
// A definitive miss (found=false, nil error) is negative-cached and returned as
// empty with a nil error. A fetch error is neither cached nor swallowed: the
// empty value is returned with an error wrapping ErrDegraded, so callers can
// tell "retry later" from "there is truly nothing here". An empty nameKey is no
// key at all, so the lookup runs uncached rather than sharing one entry with
// every other entity whose name normalizes away.
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

// kindNameKey builds the "kind + artist + title" cache key shared by the Deezer,
// Last.fm and lyrics enrichment name-keyed caches. The kind partitions the key
// rather than naming the entity, so it cannot make an unkeyable name keyable.
func kindNameKey(kind domain.ResultKind, artist, title string) string {
	nameKey := textnorm.NameKey(artist, title)
	if nameKey == "" {
		return ""
	}
	return kind.String() + textnorm.KeySeparator + nameKey
}
