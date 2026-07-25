package enrich

import "context"

func resolveThenLookup[ID comparable, T any](
	ctx context.Context,
	resolve func(context.Context) (ID, error),
	lookup func(context.Context, ID) (T, error),
	isEmpty func(T) bool,
) (T, bool, error) {
	var zero T
	var zeroID ID

	id, err := resolve(ctx)
	if err != nil {
		return zero, false, err
	}
	if id == zeroID {
		return zero, false, nil
	}

	v, err := lookup(ctx, id)
	if err != nil {
		return zero, false, err
	}
	if isEmpty(v) {
		return zero, false, nil
	}
	return v, true, nil
}
