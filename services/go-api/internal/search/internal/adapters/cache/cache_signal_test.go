package cache_test

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/shared/logging"
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
	logs  *logging.Capture
}

func (p *signalProbe) signal() *cache.Signal {
	return cache.NewSignal(p.ops, func() time.Time { return p.clock })
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
		logs:  logging.CaptureText(t, slog.LevelInfo),
	}
	return probe
}

func TestCacheSignal_RedisNilIsCountedMissAndNotLogged(t *testing.T) {
	probe := newSignalProbe(t)
	c := cache.NewRedisDeezerEnrichmentCache(scriptedClient(t, replyErr(goredis.Nil)), cache.WithSignal(probe.signal()))

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
	c := cache.NewRedisDeezerEnrichmentCache(scriptedClient(t, replyErr(errors.New("connection refused"))), cache.WithSignal(probe.signal()))

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
	c := cache.NewRedisDeezerEnrichmentCache(scriptedClient(t, replyString("{not json")), cache.WithSignal(probe.signal()))

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
	c := cache.NewRedisDeezerEnrichmentCache(scriptedClient(t, replyErr(errors.New("readonly replica"))), cache.WithSignal(probe.signal()))

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
	failing := cache.NewRedisEnrichmentCache(scriptedClient(t, replyErr(errors.New("timeout"))), cache.WithSignal(probe.signal()))
	if _, err := failing.GetNegative(context.Background(), domain.ResultKindAlbum, "x"); err == nil {
		t.Error("GetNegative swallowed the redis error")
	}
	missing := cache.NewRedisEnrichmentCache(scriptedClient(t, replyErr(goredis.Nil)), cache.WithSignal(probe.signal()))
	if got, err := missing.GetNegative(context.Background(), domain.ResultKindAlbum, "x"); got || err != nil {
		t.Errorf("GetNegative on redis.Nil = %v, %v, want false, nil", got, err)
	}
}

func TestCacheSignal_ErrorLogRateLimitedPerCacheAndOpThenResumes(t *testing.T) {
	probe := newSignalProbe(t)
	sig := probe.signal()
	failing := scriptedClient(t, replyErr(errors.New("connection refused")))
	enrichment := cache.NewRedisDeezerEnrichmentCache(failing, cache.WithSignal(sig))
	lyrics := cache.NewRedisDeezerLyricsCache(failing, cache.WithSignal(sig))
	ctx := context.Background()
	start := probe.clock
	logged := func() int { return strings.Count(probe.logs.String(), "discovery.cache_error") }

	for range 3 {
		enrichment.Get(ctx, "q")
	}
	if n := logged(); n != 1 {
		t.Fatalf("logged %d lines for repeated get failures inside the interval, want 1", n)
	}
	_ = enrichment.SetNegative(ctx, "q")
	lyrics.Get(ctx, "q")
	if n := logged(); n != 3 {
		t.Fatalf("logged %d lines, want 3: a new op and a new cache each log once", n)
	}
	probe.clock = start.Add(30*time.Second - time.Millisecond)
	enrichment.Get(ctx, "q")
	if n := logged(); n != 3 {
		t.Fatalf("logged %d lines just inside the 30s interval, want 3", n)
	}
	probe.clock = start.Add(30 * time.Second)
	enrichment.Get(ctx, "q")
	if n := logged(); n != 4 {
		t.Errorf("logged %d lines once the 30s interval passed, want 4", n)
	}
	if got := probe.counter("discovery:dzenrich.error"); got != "6" {
		t.Errorf("enrichment error counter = %s, want 6 (every failure counts)", got)
	}
	if got := probe.counter("discovery:dzlyrics.error"); got != "1" {
		t.Errorf("lyrics error counter = %s, want 1", got)
	}
}

func identityNormalize(term string) string { return term }

func TestCacheSignal_VocabularyTrigramReadFailureIsLoggedAndCounted(t *testing.T) {
	probe := newSignalProbe(t)
	hook := scriptedRedis{
		single:   func(goredis.Cmder) {},
		pipeline: func([]goredis.Cmder) error { return errors.New("pipeline broke") },
	}
	store := newVocabularyStore(t, scriptedClient(t, hook), identityNormalize, cache.NoMetaphone, cache.WithVocabSignal(probe.signal()))

	if _, err := store.FindClosest(context.Background(), "abcd", 5); err != nil {
		t.Fatalf("FindClosest err = %v, want degraded nil", err)
	}
	if !strings.Contains(probe.logs.String(), "cache=discovery:vocab") {
		t.Errorf("trigram failure not logged: %q", probe.logs.String())
	}
	if got := probe.counter("discovery:vocab.error"); got != "1" {
		t.Errorf("error counter = %s, want 1", got)
	}
}

func TestCacheSignal_VocabularyMetaphoneReadFailureIsLoggedAndCounted(t *testing.T) {
	probe := newSignalProbe(t)
	hook := scriptedRedis{single: func(cmd goredis.Cmder) { cmd.SetErr(errors.New("smembers broke")) }}
	store := newVocabularyStore(t, scriptedClient(t, hook), identityNormalize, func(string) string { return "ABCT" }, cache.WithVocabSignal(probe.signal()))

	if _, err := store.FindClosest(context.Background(), "abcd", 5); err != nil {
		t.Fatalf("FindClosest err = %v, want degraded nil", err)
	}
	if got := probe.counter("discovery:vocab.error"); got != "1" {
		t.Errorf("error counter = %s, want 1", got)
	}
}
