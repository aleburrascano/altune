package httputil

type List[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

func NewList[T any](items []T) List[T] {
	return List[T]{Items: items, Total: len(items)}
}

func MapSlice[A, B any](xs []A, f func(A) B) []B {
	out := make([]B, len(xs))
	for i, x := range xs {
		out[i] = f(x)
	}
	return out
}
