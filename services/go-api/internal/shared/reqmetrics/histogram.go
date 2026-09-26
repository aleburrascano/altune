// Recording is allocation-free on the hot path: an already-seen route resolves
// through a sync.Map load and updates fixed-size atomic counters, so concurrent
// requests never contend on a lock and never allocate. The set of distinct
// routes is bounded (maxRoutes); a route beyond the cap folds into one shared
// "overflow" key, so a hostile caller cannot grow memory without limit. The
// status dimension is a fixed three-slot array, so counting a status can never
// grow the key set whatever value the response carried.
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

// Status-class slots in routeHist.statusClass. Only 2xx, 4xx, and 5xx are
// counted; any other class (1xx, 3xx, or a status outside 100-599) is recorded
// in the latency histogram but tallied into no status slot.
const (
	class2xx = iota
	class4xx
	class5xx
	numStatusClasses
)

// routeHist is one route's latency distribution and status-class tally. Every
// field is updated with atomic ops so concurrent requests need no lock.
type routeHist struct {
	buckets     [numBuckets]uint64
	statusClass [numStatusClasses]uint64
	sumMs       uint64
	count       uint64
}

func (h *routeHist) observe(ms int64, status int) {
	atomic.AddUint64(&h.buckets[bucketIndex(ms)], 1)
	atomic.AddUint64(&h.sumMs, uint64(ms))
	atomic.AddUint64(&h.count, 1)
	if i := statusClassIndex(status); i >= 0 {
		atomic.AddUint64(&h.statusClass[i], 1)
	}
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

// statusClassIndex maps an HTTP status to its slot in routeHist.statusClass, or
// -1 for a status this view does not tally (1xx, 3xx, or anything outside
// 100-599). Total over every int, so a nonsense status cannot index out of range.
func statusClassIndex(status int) int {
	switch status / 100 {
	case 2:
		return class2xx
	case 4:
		return class4xx
	case 5:
		return class5xx
	default:
		return -1
	}
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

func (reg *registry) observe(route string, d time.Duration, status int) {
	ms := d.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	reg.hist(route).observe(ms, status)
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

// Observe records that a request matching route took d and answered with the
// given HTTP status. An empty route (an unmatched request with no chi pattern)
// folds into the shared unmatched key; a status outside 2xx/4xx/5xx counts
// toward latency but no status class. Safe for concurrent use and
// allocation-free for an already-seen route.
func Observe(route string, d time.Duration, status int) {
	if route == "" {
		route = unmatchedRoute
	}
	defaultRegistry.observe(route, d, status)
}
