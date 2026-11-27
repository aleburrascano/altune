package telemetry

import (
	"sort"
	"sync"
	"sync/atomic"
)

type Counter struct{ n atomic.Int64 }

func (c *Counter) Add(delta int64) { c.n.Add(delta) }

func (c *Counter) Value() int64 { return c.n.Load() }

type Gauge struct{ n atomic.Int64 }

func (g *Gauge) Set(value int64) { g.n.Store(value) }

func (g *Gauge) Value() int64 { return g.n.Load() }

type LabeledCounter struct {
	mu     sync.Mutex
	counts map[string]*Counter
}

func (l *LabeledCounter) Add(label string, delta int64) {
	l.mu.Lock()
	c, ok := l.counts[label]
	if !ok {
		c = &Counter{}
		l.counts[label] = c
	}
	l.mu.Unlock()
	c.Add(delta)
}

func (l *LabeledCounter) Values() map[string]int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make(map[string]int64, len(l.counts))
	for label, c := range l.counts {
		out[label] = c.Value()
	}
	return out
}

type Histogram struct {
	bounds  []int64
	buckets []atomic.Uint64
	sum     atomic.Uint64
	count   atomic.Uint64
}

type histogramSnapshot struct {
	Bounds  []int64
	Buckets []uint64
	Sum     uint64
	Count   uint64
}

func NewHistogram(bounds []int64) *Histogram {
	if !sort.SliceIsSorted(bounds, func(i, j int) bool { return bounds[i] < bounds[j] }) {
		panic("telemetry.NewHistogram: bounds must be ascending")
	}
	return &Histogram{bounds: append([]int64(nil), bounds...), buckets: make([]atomic.Uint64, len(bounds)+1)}
}

func (h *Histogram) Observe(value int64) {
	if value < 0 {
		value = 0
	}
	h.buckets[sort.Search(len(h.bounds), func(i int) bool { return value <= h.bounds[i] })].Add(1)
	h.sum.Add(uint64(value))
	h.count.Add(1)
}

func (h *Histogram) Snapshot() histogramSnapshot {
	buckets := make([]uint64, len(h.buckets))
	for i := range h.buckets {
		buckets[i] = h.buckets[i].Load()
	}
	return histogramSnapshot{Bounds: append([]int64(nil), h.bounds...), Buckets: buckets, Sum: h.sum.Load(), Count: h.count.Load()}
}

type Registry struct {
	mu         sync.Mutex
	counters   map[string]*Counter
	gauges     map[string]*Gauge
	labeled    map[string]*LabeledCounter
	histograms map[string]*Histogram
}

func NewRegistry() *Registry {
	return &Registry{
		counters:   map[string]*Counter{},
		gauges:     map[string]*Gauge{},
		labeled:    map[string]*LabeledCounter{},
		histograms: map[string]*Histogram{},
	}
}

func getOrCreate[T any](mu *sync.Mutex, into map[string]*T, name string, create func() *T) *T {
	mu.Lock()
	defer mu.Unlock()
	if existing, ok := into[name]; ok {
		return existing
	}
	made := create()
	into[name] = made
	return made
}

func (r *Registry) Counter(name string) *Counter {
	return getOrCreate(&r.mu, r.counters, name, func() *Counter { return &Counter{} })
}

func (r *Registry) Gauge(name string) *Gauge {
	return getOrCreate(&r.mu, r.gauges, name, func() *Gauge { return &Gauge{} })
}

func (r *Registry) LabeledCounter(name string) *LabeledCounter {
	return getOrCreate(&r.mu, r.labeled, name, func() *LabeledCounter {
		return &LabeledCounter{counts: map[string]*Counter{}}
	})
}

func (r *Registry) Histogram(name string, bounds []int64) *Histogram {
	return getOrCreate(&r.mu, r.histograms, name, func() *Histogram { return NewHistogram(bounds) })
}
