package core

import (
	"fmt"
	"sort"
	"sync"
)

type Registry struct {
	mu      sync.RWMutex
	byID    map[string]Bucket
	ordered []Bucket
}

func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]Bucket)}
}

func (r *Registry) Register(b Bucket) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id := b.Meta().ID
	if id == "" {
		panic("core: bucket registered with empty ID")
	}
	if _, exists := r.byID[id]; exists {
		panic(fmt.Sprintf("core: bucket %q registered twice", id))
	}
	r.byID[id] = b
	r.ordered = append(r.ordered, b)
}

func (r *Registry) Buckets() []Bucket {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Bucket, len(r.ordered))
	copy(out, r.ordered)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Meta().ID < out[j].Meta().ID
	})
	return out
}

func (r *Registry) Get(id string) (Bucket, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.byID[id]
	if !ok {
		return nil, false
	}
	return b, true
}

var Default = NewRegistry()

func Register(b Bucket) {
	Default.Register(b)
}
