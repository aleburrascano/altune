package memory

import (
	"context"
	"sync"
)

type NameKeyedCache[T any] struct {
	mu   sync.Mutex
	pos  map[string]T
	neg  map[string]bool
	sets int
	negs int
}

func NewNameKeyedCache[T any]() *NameKeyedCache[T] {
	return &NameKeyedCache[T]{pos: map[string]T{}, neg: map[string]bool{}}
}

func (c *NameKeyedCache[T]) Get(_ context.Context, nameKey string) (T, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.pos[nameKey]
	return v, ok, nil
}

func (c *NameKeyedCache[T]) Set(_ context.Context, nameKey string, v T) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sets++
	c.pos[nameKey] = v
	return nil
}

func (c *NameKeyedCache[T]) GetNegative(_ context.Context, nameKey string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.neg[nameKey], nil
}

func (c *NameKeyedCache[T]) SetNegative(_ context.Context, nameKey string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.negs++
	c.neg[nameKey] = true
	return nil
}

func (c *NameKeyedCache[T]) Seed(nameKey string, v T) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pos[nameKey] = v
}

func (c *NameKeyedCache[T]) SeedNegative(nameKey string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.neg[nameKey] = true
}

func (c *NameKeyedCache[T]) Sets() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sets
}

func (c *NameKeyedCache[T]) Negatives() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.negs
}

func (c *NameKeyedCache[T]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.pos)
}
