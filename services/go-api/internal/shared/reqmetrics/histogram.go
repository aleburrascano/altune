package reqmetrics

import (
	"sync"
	"sync/atomic"
	"time"
)

var bucketUpperBoundsMs = [...]int64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000}

const numBuckets = len(bucketUpperBoundsMs) + 1

const (
	unmatchedRoute = "unmatched"
	overflowRoute  = "overflow"
	maxRoutes      = 256
)

const (
	class2xx = iota
	class4xx
	class5xx
	numStatusClasses
)

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

func bucketIndex(ms int64) int {
	for i, bound := range bucketUpperBoundsMs {
		if ms <= bound {
			return i
		}
	}
	return numBuckets - 1
}

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

type registry struct {
	routes sync.Map
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

var defaultRegistry = newRegistry()

func Observe(route string, d time.Duration, status int) {
	if route == "" {
		route = unmatchedRoute
	}
	defaultRegistry.observe(route, d, status)
}
