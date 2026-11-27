package registry

import (
	"altune/go-api/internal/shared/telemetry"
	"sync"
	"sync/atomic"
	"time"
)

var bucketUpperBoundsMs = []int64{1, 5, 10, 25, 50, 100, 250, 500, 1000, 2500, 5000}

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
	latency     *telemetry.Histogram
	statusClass [numStatusClasses]uint64
}

func newRouteHist() *routeHist {
	return &routeHist{latency: telemetry.NewHistogram(bucketUpperBoundsMs)}
}

func (h *routeHist) observe(ms int64, status int) {
	h.latency.Observe(ms)
	if i := statusClassIndex(status); i >= 0 {
		atomic.AddUint64(&h.statusClass[i], 1)
	}
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

type Registry struct {
	routes sync.Map
	n      atomic.Int32
}

func New() *Registry {
	reg := &Registry{}
	reg.routes.Store(unmatchedRoute, newRouteHist())
	reg.routes.Store(overflowRoute, newRouteHist())
	return reg
}

func (reg *Registry) Observe(route string, d time.Duration, status int) {
	if route == "" {
		route = unmatchedRoute
	}
	ms := d.Milliseconds()
	if ms < 0 {
		ms = 0
	}
	reg.hist(route).observe(ms, status)
}

func (reg *Registry) hist(route string) *routeHist {
	if v, ok := reg.routes.Load(route); ok {
		return v.(*routeHist)
	}
	if reg.n.Load() >= maxRoutes {
		return reg.mustLoad(overflowRoute)
	}
	h, loaded := reg.routes.LoadOrStore(route, newRouteHist())
	if !loaded {
		reg.n.Add(1)
	}
	return h.(*routeHist)
}

func (reg *Registry) mustLoad(route string) *routeHist {
	v, _ := reg.routes.Load(route)
	return v.(*routeHist)
}
