package core

import (
	"fmt"
	"sort"
	"sync"
)

// Registry holds the registered buckets. The shell core depends only on this
// type and on the Bucket interface, never on a concrete bucket, so a new bucket
// slots in without the core changing.
type Registry struct {
	mu      sync.RWMutex
	byID    map[string]Bucket
	ordered []Bucket
}

// NewRegistry returns an empty registry. Production uses the package-global
// Default; tests build isolated registries with this constructor.
func NewRegistry() *Registry {
	return &Registry{byID: make(map[string]Bucket)}
}

// Register adds a bucket. It panics on a missing ID or a duplicate: both are
// programmer errors that must fail loudly at startup, not silently drop a panel.
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

// Buckets returns the registered buckets in a stable, ID-sorted order so the
// shell renders panels deterministically regardless of registration order.
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

// Default is the process-wide registry. Buckets self-register into it from their
// package init, and the composition root activates them with one import line.
var Default = NewRegistry()

// Register adds a bucket to the Default registry.
func Register(b Bucket) {
	Default.Register(b)
}
