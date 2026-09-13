package httputil

// List is the standard JSON envelope for list endpoints: the items plus a
// Total count. It replaces the per-endpoint {Items []T; Total int} structs so
// every list response shares one shape. Endpoints whose Total is not len(Items)
// or that carry extra fields (pagination cursors, per-section flags) keep their
// own named type instead.
type List[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

// NewList wraps items in a List with Total set to len(items). The items slice is
// stored as-is, so a non-nil empty slice still serializes to [] (not null).
func NewList[T any](items []T) List[T] {
	return List[T]{Items: items, Total: len(items)}
}

// MapSlice returns a new slice with f applied to each element of xs. The result
// is always non-nil (len 0 for an empty input), matching the make([]B, len(xs))
// idiom the list handlers previously inlined.
func MapSlice[A, B any](xs []A, f func(A) B) []B {
	out := make([]B, len(xs))
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}
