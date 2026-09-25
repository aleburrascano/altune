package cache

import (
	"context"
	"expvar"
	"log/slog"
	"strings"
	"sync"
	"time"
)

const (
	outcomeHit   = "hit"
	outcomeMiss  = "miss"
	outcomeError = "error"

	errorLogInterval = 30 * time.Second
)

var (
	cacheOps = expvar.NewMap("discovery_cache_ops")

	errorLogMu   sync.Mutex
	errorLogLast = map[string]time.Time{}
)

func cacheKind(key string) string {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) < 3 {
		return "unknown"
	}
	return parts[0] + ":" + parts[1]
}

func countOutcome(key, outcome string) {
	cacheOps.Add(cacheKind(key)+"."+outcome, 1)
}

func logCacheError(ctx context.Context, key, op string, err error) {
	kind := cacheKind(key)
	countOutcome(key, outcomeError)
	if !errorLogDue(kind + "." + op) {
		return
	}
	slog.WarnContext(ctx, "discovery.cache_error",
		slog.String("cache", kind),
		slog.String("op", op),
		slog.Any("error", err),
	)
}

func errorLogDue(bucket string) bool {
	errorLogMu.Lock()
	defer errorLogMu.Unlock()
	now := time.Now()
	if now.Sub(errorLogLast[bucket]) < errorLogInterval {
		return false
	}
	errorLogLast[bucket] = now
	return true
}
