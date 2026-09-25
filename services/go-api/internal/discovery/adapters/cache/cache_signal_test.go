package cache

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func captureLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	errorLogMu.Lock()
	errorLogLast = map[string]time.Time{}
	errorLogMu.Unlock()
	return &buf
}

func TestUnreachableRedis_LogsAndCountsErrorInsteadOfSilentMiss(t *testing.T) {
	buf := captureLogs(t)
	c := NewRedisDeezerEnrichmentCache(unreachableRedisClient(t))
	before := cacheOps.Get("discovery:dzenrich.error")

	_, hit, err := c.Get(context.Background(), "secret query")
	if hit || err != nil {
		t.Fatalf("Get = hit %v err %v, want miss", hit, err)
	}
	if !strings.Contains(buf.String(), "discovery.cache_error") {
		t.Errorf("no error log emitted: %q", buf.String())
	}
	if strings.Contains(buf.String(), "secret query") {
		t.Errorf("log leaks raw key: %q", buf.String())
	}
	if cacheOps.Get("discovery:dzenrich.error") == before {
		t.Error("error counter not incremented")
	}
}

func TestUnreachableRedis_GetNegativeReturnsError(t *testing.T) {
	captureLogs(t)
	c := NewRedisDeezerEnrichmentCache(unreachableRedisClient(t))
	if _, err := c.GetNegative(context.Background(), "x"); err == nil {
		t.Error("GetNegative swallowed the redis error")
	}
}

func TestErrorLog_RateLimitedPerCacheAndOp(t *testing.T) {
	buf := captureLogs(t)
	for range 3 {
		logCacheError(context.Background(), "discovery:x:v1:k", "get", context.Canceled)
	}
	if n := strings.Count(buf.String(), "discovery.cache_error"); n != 1 {
		t.Errorf("logged %d lines, want 1", n)
	}
}
