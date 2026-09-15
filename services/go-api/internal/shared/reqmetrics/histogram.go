// Package reqmetrics is a bounded, fixed-bucket histogram of per-route request
// latency. It backs the latency view of the operator-only
// GET /admin/metrics/live endpoint.
//
// Recording is allocation-free on the hot path: an already-seen route resolves
// through a sync.Map load and updates fixed-size atomic counters, so concurrent
// requests never contend on a lock and never allocate. The set of distinct
// routes is bounded (maxRoutes); a route beyond the cap folds into one shared
// "overflow" key, so a hostile caller cannot grow memory without limit.
package reqmetrics

import (
	"sync"
	"sync/atomic"
	"time"
)

// bucketUpperBoundsMs are the inclusive upper bounds, in milliseconds, of the
// fixed latency buckets. A final implicit bucket (index len) catches anything
// slower than the last bound.
var bucketUpperBoundsMs = [...]int64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000}

// numBuckets counts the fixed buckets plus the final overflow bucket.
const numBuckets = len(bucketUpperBoundsMs) + 1

// Reserved route keys and the cardinality cap. Unmatched requests (404s with no
// chi pattern) share one key, and routes past maxRoutes fold into another, so
// neither a hostile client nor an unmatched flood can expand the key set.
const (
	unmatchedRoute = "unmatched"
	overflowRoute  = "overflow"
	maxRoutes      = 256
)

// routeHist is one route's latency distribution. Every field is updated with
// atomic ops so concurrent requests need no lock.
type routeHist struct {
	buckets [numBuckets]uint64
	sumMs   uint64
	count   uint64
}

func (h *routeHist) observe(ms int64) {
	atomic.AddUint64(&h.buckets[bucketIndex(ms)], 1)
	atomic.AddUint64(&h.sumMs, uint64(ms))
	atomic.AddUint64(&h.count, 1)
}

// bucketIndex returns the fixed bucket a latency of ms milliseconds falls in.
func bucketIndex(ms int64) int {
	for i, bound := range bucketUpperBoundsMs {
		if ms <= bound {
			return i
		}
	}
	return numBuckets - 1
}

// registry maps a route key to its histogram. A lookup of a known route is
// lock-free; a first sighting stores under sync.Map's own synchronization.
type registry struct {
	routes sync.Map // string -> *routeHist
	n      atomic.Int32
}

func newRegistry() *registry {
	reg := &registry{}
	reg.routes.Store(unmatchedRoute, &routeHist{})
	reg.routes.Store(overflowRoute, &routeHist{})
	return reg
}

func (reg *registry) observe(route string, d time.Duration) {
	ms := d.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	reg.hist(route).observe(ms)
}

func (reg *registry) hist(route string) *routeHist {
	if v, ok := reg.routes.Load(route); ok {
		return v.(*routeHist)
	}
	if reg.n.Load() >= maxRoutes {
		return reg.mustLoad(overflowRoute)
	}
	h, loaded := reg.routes.LoadOrStore(route, &routeHist{})
	if !loaded {
		reg.n.Add(1)
	}
	return h.(*routeHist)
}

func (reg *registry) mustLoad(route string) *routeHist {
	v, _ := reg.routes.Load(route)
	return v.(*routeHist)
}

// defaultRegistry is the process-wide store the middleware records into and the
// endpoint reads from.
var defaultRegistry = newRegistry()

// Observe records that a request matching route took d. An empty route (an
// unmatched request with no chi pattern) folds into the shared unmatched key.
// Safe for concurrent use and allocation-free for an already-seen route.
func Observe(route string, d time.Duration) {
	if route == "" {
		route = unmatchedRoute
	}
	defaultRegistry.observe(route, d)
}
