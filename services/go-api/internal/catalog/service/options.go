package service

func applyOptions[T any](t *T, opts []func(*T)) *T {
	for _, opt := range opts {
		opt(t)
	}
	return t
}
