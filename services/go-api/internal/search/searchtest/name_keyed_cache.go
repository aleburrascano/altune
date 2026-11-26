package searchtest

import "altune/go-api/internal/search/internal/adapters/memory"

type NameKeyedCache[T any] = memory.NameKeyedCache[T]

func NewNameKeyedCache[T any]() *NameKeyedCache[T] {
	return memory.NewNameKeyedCache[T]()
}
