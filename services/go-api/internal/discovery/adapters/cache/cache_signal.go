package cache

import (
	"context"
	"expvar"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type CacheKind string

type outcome string

type cacheOp string

const (
	outcomeHit   outcome = "hit"
	outcomeMiss  outcome = "miss"
	outcomeError outcome = "error"

	opGet    cacheOp = "get"
	opSet    cacheOp = "set"
	opDecode cacheOp = "decode"
	opEncode cacheOp = "encode"

	kindUnknown CacheKind = "unknown"
	kindVocab   CacheKind = "discovery:vocab"

	errorLogInterval = 30 * time.Second
)

type Signal struct {
	ops     *expvar.Map
	now     func() time.Time
	mu      sync.Mutex
	lastLog map[string]time.Time
}

func NewSignal(ops *expvar.Map, now func() time.Time) *Signal {
	return &Signal{ops: ops, now: now, lastLog: map[string]time.Time{}}
}

func newDefaultSignal() *Signal {
	return NewSignal(new(expvar.Map).Init(), time.Now)
}

type Option func(*redisJSON)

func WithSignal(s *Signal) Option {
	return func(r *redisJSON) { r.signal = s }
}

func kindOf(key string) CacheKind {
	parts := strings.SplitN(key, ":", 3)
	if len(parts) < 3 {
		return kindUnknown
	}
	return CacheKind(parts[0] + ":" + parts[1])
}

func (s *Signal) count(kind CacheKind, result outcome) {
	s.ops.Add(string(kind)+"."+string(result), 1)
}

func (s *Signal) failure(ctx context.Context, kind CacheKind, op cacheOp, err error) {
	s.count(kind, outcomeError)
	if !s.logDue(string(kind) + "." + string(op)) {
		return
	}
	slog.WarnContext(ctx, "discovery.cache_error",
		slog.String("cache", string(kind)),
		slog.String("op", string(op)),
		slog.Any("error", err),
	)
}

func (s *Signal) logDue(bucket string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if now.Sub(s.lastLog[bucket]) < errorLogInterval {
		return false
	}
	s.lastLog[bucket] = now
	return true
}
