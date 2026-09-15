package service

// applyOptions applies each functional option to t in order and returns t.
// Service constructors build their defaults, then delegate here so the
// option-application policy lives in one place.
func applyOptions[T any](t *T, opts []func(*T)) *T {
	for _, opt := range opts {
		opt(t)
	}
	return t
}
