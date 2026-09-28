package shell

import (
	"altune/overseer/internal/core"
	"log/slog"
	"sync"
	"time"
)

const (
	sparkPoints  = 30
	sparkWindow  = time.Hour
	sparkRefresh = time.Minute
)

type TailReader interface {
	Tail(bucket, series string, from, to time.Time, limit int) ([]core.Point, error)
}

func (h *Handler) spark(b core.Bucket, id string) (spark []core.SparkPoint) {
	defer func() {
		if rec := recover(); rec != nil {
			slog.Error("overseer.shell.spark_panic", "bucket", id, "recover", rec)
			spark = nil
		}
	}()
	keyed, ok := b.(core.KeySeries)
	if !ok {
		return nil
	}
	name := keyed.KeySeries()
	if name == "" {
		return nil
	}
	return h.sparkCache.load(id, func() ([]core.SparkPoint, error) {
		return h.readSpark(id, name)
	})
}

func (h *Handler) readSpark(id, name string) ([]core.SparkPoint, error) {
	to := time.Now().UTC()
	points, err := h.queryTail(id, name, to.Add(-sparkWindow), to)
	if err != nil {
		return nil, err
	}
	spark := make([]core.SparkPoint, len(points))
	for i, p := range points {
		spark[i] = core.SparkPoint{At: p.At, V: p.Value}
	}
	return spark, nil
}

func (h *Handler) queryTail(bucket, series string, from, to time.Time) ([]core.Point, error) {
	if tail, ok := h.series.(TailReader); ok {
		return tail.Tail(bucket, series, from, to, sparkPoints)
	}
	points, err := h.series.Query(bucket, series, from, to)
	if err != nil {
		return nil, err
	}
	if len(points) > sparkPoints {
		points = points[len(points)-sparkPoints:]
	}
	return points, nil
}

type sparkEntry struct {
	mu        sync.Mutex
	points    []core.SparkPoint
	fetchedAt time.Time
	failing   bool
}

type sparkCache struct {
	mu      sync.Mutex
	entries map[string]*sparkEntry
	now     func() time.Time
}

func newSparkCache() *sparkCache {
	return &sparkCache{entries: map[string]*sparkEntry{}, now: time.Now}
}

func (c *sparkCache) entry(id string) *sparkEntry {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok {
		e = &sparkEntry{}
		c.entries[id] = e
	}
	return e
}

func (c *sparkCache) load(id string, read func() ([]core.SparkPoint, error)) []core.SparkPoint {
	e := c.entry(id)
	e.mu.Lock()
	defer e.mu.Unlock()

	now := c.now()
	if !e.fetchedAt.IsZero() && now.Sub(e.fetchedAt) < sparkRefresh {
		return e.points
	}

	points, err := read()
	if err != nil {
		if !e.failing {
			e.failing = true
			slog.Warn("overseer.shell.spark_read_failed", "bucket", id, "error", err)
		}
		return e.points
	}
	if e.failing {
		e.failing = false
		slog.Info("overseer.shell.spark_read_recovered", "bucket", id)
	}
	e.points, e.fetchedAt = points, now
	return e.points
}
