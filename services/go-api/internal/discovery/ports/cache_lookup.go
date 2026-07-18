package ports

import "context"

// CachedLookup is the read-through cache dance shared by every name-resolved
// detail enricher (Deezer, Last.fm, Discogs album/artist, lyrics). It is the
// single place — and single test surface — for "positive hit → return; negative
// hit → empty; otherwise fetch and record the outcome".
//
// fetch returns one of three outcomes, mapped from the provider's resolve+lookup:
//
//   - (value, true,  nil)  → a hit; positive-cached and returned.
//   - (_,     false, nil)  → a definitive miss (no id, or a zero value); the name
//     is negative-cached so it is not re-resolved every open.
//   - (_,     false, err)  → a transient failure (network/auth); NOT cached, so a
//     later open can retry. fetch logs it; CachedLookup swallows it (the
//     detail endpoint is best-effort and always answers with empty).
//
// cache may be nil (the service then runs uncached). empty is returned on every
// non-hit path so the caller never sees a partially-populated zero value.
func CachedLookup[T any](
	ctx context.Context,
	cache NameKeyedCache[T],
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
		return empty, nil // transient; logged by fetch, not cached
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
