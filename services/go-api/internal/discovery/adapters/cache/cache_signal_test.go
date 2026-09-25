package cache

import (
	"altune/go-api/internal/discovery/domain"
	"bytes"
	"context"
	"errors"
	"expvar"
	"log/slog"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

type scriptedRedis struct {
	single   func(cmd goredis.Cmder)
	pipeline func(cmds []goredis.Cmder) error
}

func (scriptedRedis) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (h scriptedRedis) ProcessHook(goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		h.single(cmd)
		return cmd.Err()
	}
}

func (h scriptedRedis) ProcessPipelineHook(goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		if h.pipeline == nil {
			return nil
		}
		return h.pipeline(cmds)
	}
}

func scriptedClient(t *testing.T, hook scriptedRedis) *goredis.Client {
	t.Helper()
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1"})
	client.AddHook(hook)
	t.Cleanup(func() { client.Close() })
	return client
}

func replyErr(err error) scriptedRedis {
	return scriptedRedis{single: func(cmd goredis.Cmder) { cmd.SetErr(err) }}
}

func replyString(val string) scriptedRedis {
	return scriptedRedis{single: func(cmd goredis.Cmder) {
		if s, ok := cmd.(*goredis.StringCmd); ok {
			s.SetVal(val)
		}
	}}
}

type signalProbe struct {
	ops   *expvar.Map
	clock time.Time
	logs  *bytes.Buffer
}

func (p *signalProbe) signal() *Signal {
	return NewSignal(p.ops, func() time.Time { return p.clock })
}

func (p *signalProbe) counter(name string) string {
	if v := p.ops.Get(name); v != nil {
		return v.String()
	}
	return "0"
}

func newSignalProbe(t *testing.T) *signalProbe {
	t.Helper()
	probe := &signalProbe{
		ops:   new(expvar.Map).Init(),
		clock: time.Unix(1_700_000_000, 0),
		logs:  &bytes.Buffer{},
	}
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(probe.logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	return probe
}

func TestCacheSignal_RedisNilIsCountedMissAndNotLogged(t *testing.T) {
	probe := newSignalProbe(t)
	c := NewRedisDeezerEnrichmentCache(scriptedClient(t, replyErr(goredis.Nil)), WithSignal(probe.signal()))

	_, hit, err := c.Get(context.Background(), "some query")
	if hit || err != nil {
		t.Fatalf("Get = hit %v err %v, want clean miss", hit, err)
	}
	if probe.logs.Len() != 0 {
		t.Errorf("redis.Nil was logged: %q", probe.logs.String())
	}
	if got := probe.counter("discovery:dzenrich.miss"); got != "1" {
		t.Errorf("miss counter = %s, want 1", got)
	}
	if got := probe.counter("discovery:dzenrich.error"); got != "0" {
		t.Errorf("error counter = %s, want 0", got)
	}
}

func TestCacheSignal_RedisFailureIsLoggedAndCountedWithoutLeakingKey(t *testing.T) {
	probe := newSignalProbe(t)
	c := NewRedisDeezerEnrichmentCache(scriptedClient(t, replyErr(errors.New("connection refused"))), WithSignal(probe.signal()))

	_, hit, err := c.Get(context.Background(), "secret query")
	if hit || err != nil {
		t.Fatalf("Get = hit %v err %v, want degraded miss", hit, err)
	}
	if !strings.Contains(probe.logs.String(), "discovery.cache_error") {
		t.Errorf("no error log emitted: %q", probe.logs.String())
	}
	if strings.Contains(probe.logs.String(), "secret query") {
		t.Errorf("log leaks raw key: %q", probe.logs.String())
	}
	if got := probe.counter("discovery:dzenrich.error"); got != "1" {
		t.Errorf("error counter = %s, want 1", got)
	}
}

func TestCacheSignal_CorruptEntryIsLoggedAsDecodeErrorAndMisses(t *testing.T) {
	probe := newSignalProbe(t)
	c := NewRedisDeezerEnrichmentCache(scriptedClient(t, replyString("{not json")), WithSignal(probe.signal()))

	_, hit, err := c.Get(context.Background(), "some query")
	if hit || err != nil {
		t.Fatalf("Get = hit %v err %v, want miss", hit, err)
	}
	if !strings.Contains(probe.logs.String(), "op=decode") {
		t.Errorf("decode failure not logged: %q", probe.logs.String())
	}
	if got := probe.counter("discovery:dzenrich.error"); got != "1" {
		t.Errorf("error counter = %s, want 1", got)
	}
}

func TestCacheSignal_SetFailureReturnsErrorAndIsLogged(t *testing.T) {
	probe := newSignalProbe(t)
	c := NewRedisDeezerEnrichmentCache(scriptedClient(t, replyErr(errors.New("readonly replica"))), WithSignal(probe.signal()))

	if err := c.SetNegative(context.Background(), "some query"); err == nil {
		t.Fatal("SetNegative swallowed the redis error")
	}
	if !strings.Contains(probe.logs.String(), "op=set") {
		t.Errorf("set failure not logged: %q", probe.logs.String())
	}
	if got := probe.counter("discovery:dzenrich.error"); got != "1" {
		t.Errorf("error counter = %s, want 1", got)
	}
}

func TestCacheSignal_GetNegativeSurfacesErrorButNotRedisNil(t *testing.T) {
	probe := newSignalProbe(t)
	failing := NewRedisEnrichmentCache(scriptedClient(t, replyErr(errors.New("timeout"))), WithSignal(probe.signal()))
	if _, err := failing.GetNegative(context.Background(), domain.ResultKindAlbum, "x"); err == nil {
		t.Error("GetNegative swallowed the redis error")
	}
	missing := NewRedisEnrichmentCache(scriptedClient(t, replyErr(goredis.Nil)), WithSignal(probe.signal()))
	if got, err := missing.GetNegative(context.Background(), domain.ResultKindAlbum, "x"); got || err != nil {
		t.Errorf("GetNegative on redis.Nil = %v, %v, want false, nil", got, err)
	}
}

func TestCacheSignal_ErrorLogRateLimitedPerCacheAndOpThenResumes(t *testing.T) {
	probe := newSignalProbe(t)
	sig := probe.signal()
	emit := func() { sig.failure(context.Background(), "discovery:x", opGet, context.Canceled) }

	emit()
	emit()
	emit()
	if n := strings.Count(probe.logs.String(), "discovery.cache_error"); n != 1 {
		t.Fatalf("logged %d lines inside the interval, want 1", n)
	}
	probe.clock = probe.clock.Add(errorLogInterval)
	emit()
	if n := strings.Count(probe.logs.String(), "discovery.cache_error"); n != 2 {
		t.Errorf("logged %d lines after the interval, want 2", n)
	}
	if got := probe.counter("discovery:x.error"); got != "4" {
		t.Errorf("error counter = %s, want 4 (every failure counts)", got)
	}
}
