package enrich

import "context"

// resolveThenLookup is the resolve→lookup dance shared by the enrichers whose
// provider needs a separate id-resolution step before the detail fetch
// (Deezer, Discogs album, Discogs artist): resolve a name to an id, bail out
// (definitive miss, no error) on a zero id, look up the id, bail out
// (definitive miss) on an empty result. Errors from either step are returned
// as-is (transient — the caller's CachedLookup treats them as uncached).
// zero is returned on every non-hit path.
func resolveThenLookup[ID comparable, T any](
	ctx context.Context,
	resolve func(context.Context) (ID, error),
	lookup func(context.Context, ID) (T, error),
	isEmpty func(T) bool,
	onResolveErr func(error),
	onLookupErr func(id ID, err error),
) (T, bool, error) {
	var zero T
	var zeroID ID

	id, err := resolve(ctx)
	if err != nil {
		onResolveErr(err)
		return zero, false, err
	}
	if id == zeroID {
		return zero, false, nil
	}

	v, err := lookup(ctx, id)
	if err != nil {
		onLookupErr(id, err)
		return zero, false, err
	}
	if isEmpty(v) {
		return zero, false, nil
	}
	return v, true, nil
}
