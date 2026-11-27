package details

import "context"

type NoNameKeyedCache[T any] struct{}

func (NoNameKeyedCache[T]) Get(context.Context, string) (T, bool, error) {
	var zero T
	return zero, false, nil
}

func (NoNameKeyedCache[T]) Set(context.Context, string, T) error { return nil }

func (NoNameKeyedCache[T]) GetNegative(context.Context, string) (bool, error) { return false, nil }

func (NoNameKeyedCache[T]) SetNegative(context.Context, string) error { return nil }
