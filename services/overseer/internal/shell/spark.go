package shell

import (
	"altune/overseer/internal/core"
	"log/slog"
	"sync"
	"time"
)

const (
	sparkPoints = 30
	sparkWindow = time.Hour
	// sparkRefresh is how often a bucket's spark is re-read from the store.
	// Rollups are minute-grained, so refreshing more often than this buys no
	// fresher data — it would only re-run the same query against the single
	// SQLite connection every 2s SSE tick, per connected client.
	sparkRefresh = time.Minute
)

// TailReader is the optional push-down seam a SeriesReader implements when it can
// bound a query to its most recent N points in SQL rather than fetching the whole
// window and trimming in Go. The spark path is the only caller: it always wants
// the tail, never the full window, so pushing the LIMIT into the query keeps a
// long-lived series cheap to read regardless of how many rows it holds.
type TailReader interface {
	Tail(bucket, series string, from, to time.Time, limit int) ([]core.Point, error)
}

// spark builds a bucket's spark, cached and refreshed at most once per
// sparkRefresh so N clients (every SSE connection re-emitting every
// streamInterval, plus every /api/buckets GET) cost at most one store read per
// bucket per refresh window, not one per frame. It carries its own recover: a
// panic in the bucket's KeySeries() or in the underlying history read degrades
// this one bucket's spark to nil rather than the snapshot safeSnapshot already
// built, matching the degrade-don't-crash invariant on the render side.
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

// readSpark performs the actual bounded history read, tail-limited in SQL when
// the reader supports it (every production reader does; test fakes that only
// implement SeriesReader fall back to a full-window read trimmed in Go).
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

// sparkEntry holds one bucket's cached spark and the bookkeeping for a
// once-per-refresh-window read: the last successfully read spark (served on a
// failed read, never dropped), when it was fetched, and whether the current
// failure streak has already been logged.
type sparkEntry struct {
	mu        sync.Mutex
	points    []core.SparkPoint
	fetchedAt time.Time
	failing   bool
}

// sparkCache serves every bucket's spark from a small per-bucket cache refreshed
// at most once per sparkRefresh. Refreshing holds only the entry's own lock —
// never the cache's map lock, and never anything the snapshot path holds — so a
// slow or contended store read blocks at most the other callers racing to
// refresh the SAME bucket, not the whole response.
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

// load serves the bucket's cached spark, refreshing through read at most once
// per sparkRefresh. A refresh that fails logs once per failure streak (not once
// per call) and serves the last good spark, which is nil until the first
// successful read ever completes.
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
