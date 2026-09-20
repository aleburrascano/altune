package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

// commandCounter is a go-redis hook that records every command the store sends
// without touching the network, so the Redis call shape of the fuzzy path can be
// asserted in CI where no Redis server exists.
type commandCounter struct {
	mu            sync.Mutex
	single        map[string]int
	pipelines     int
	pipelinedCmds map[string]int
}

func newCountingClient(t *testing.T) (*goredis.Client, *commandCounter) {
	t.Helper()
	counter := &commandCounter{single: map[string]int{}, pipelinedCmds: map[string]int{}}
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1"})
	client.AddHook(counter)
	t.Cleanup(func() { _ = client.Close() })
	return client, counter
}

func (c *commandCounter) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (c *commandCounter) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.single[cmd.Name()]++
		cmd.SetErr(goredis.Nil)
		return goredis.Nil
	}
}

func (c *commandCounter) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.pipelines++
		for _, cmd := range cmds {
			c.pipelinedCmds[cmd.Name()]++
		}
		return nil
	}
}

func TestFindClosest_OversizedQueryIssuesBoundedPipelinedTrigramLookups(t *testing.T) {
	client, counter := newCountingClient(t)
	store := NewVocabularyStore(client, lowercaseNorm)

	hostile := strings.Repeat("qwertyuiopasdfghjklz", 500) // 10,000 runes -> 9,998 trigrams
	if _, err := store.FindClosest(context.Background(), hostile, 5); err != nil {
		t.Fatalf("FindClosest: %v", err)
	}

	if got := counter.single["smembers"]; got != 0 {
		t.Errorf("unpipelined SMEMBERS round trips = %d, want 0 (trigram lookups must be pipelined)", got)
	}
	if counter.pipelines > 1 {
		t.Errorf("pipelines = %d, want at most 1 for the trigram lookup", counter.pipelines)
	}
	if got := counter.pipelinedCmds["smembers"]; got == 0 || got > vocabTrigramLookupCap {
		t.Errorf("pipelined SMEMBERS = %d, want 1..%d", got, vocabTrigramLookupCap)
	}
}

// passthroughCounter counts SMEMBERS round trips against a real Redis.
type passthroughCounter struct {
	mu        sync.Mutex
	smembers  int
	pipelines int
}

func (c *passthroughCounter) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (c *passthroughCounter) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		c.mu.Lock()
		if cmd.Name() == "smembers" {
			c.smembers++
		}
		c.mu.Unlock()
		return next(ctx, cmd)
	}
}

func (c *passthroughCounter) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(ctx context.Context, cmds []goredis.Cmder) error {
		c.mu.Lock()
		c.pipelines++
		c.mu.Unlock()
		return next(ctx, cmds)
	}
}

func TestVocabularyStore_Redis_OversizedFuzzyQueryIsOneRoundTripAndStillMatches(t *testing.T) {
	client := testRedisClient(t)
	counter := &passthroughCounter{}
	client.AddHook(counter)
	store := NewVocabularyStore(client, lowercaseNorm)
	ctx := context.Background()

	term := "qaboundsmegaman"
	vocabCleanKeys(t, term)
	if err := store.Add(ctx, domain.VocabularyEntry{Term: "QABoundsMegaman", TermNorm: term, Kind: "track", Popularity: 1}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	counter.pipelines = 0

	if _, err := store.FindClosest(ctx, strings.Repeat("qwertyuiopasdfghjklz", 500), 5); err != nil {
		t.Fatalf("FindClosest oversized: %v", err)
	}
	if counter.smembers != 0 || counter.pipelines != 1 {
		t.Errorf("oversized query: smembers=%d pipelines=%d, want 0 and 1", counter.smembers, counter.pipelines)
	}

	got, err := store.FindClosest(ctx, "qaboundsmegamsn", 5)
	if err != nil {
		t.Fatalf("FindClosest typo: %v", err)
	}
	found := false
	for _, e := range got {
		found = found || e.TermNorm == term
	}
	if !found {
		t.Errorf("typo lookup = %+v, want %q among the pipelined candidates", got, term)
	}
}

// stubRedis answers the vocabulary store's commands from memory, so the fuzzy
// path's Redis call shape can be asserted at vocabulary scale with no server.
type stubRedis struct {
	mu         sync.Mutex
	sets       map[string]map[string]bool
	entries    map[string]string
	calls      map[string]int
	roundTrips int
	mgetKeys   int
}

func newStubRedisClient(t *testing.T) (*goredis.Client, *stubRedis) {
	t.Helper()
	stub := &stubRedis{
		sets:    map[string]map[string]bool{},
		entries: map[string]string{},
		calls:   map[string]int{},
	}
	client := goredis.NewClient(&goredis.Options{Addr: "127.0.0.1:1"})
	client.AddHook(stub)
	t.Cleanup(func() { _ = client.Close() })
	return client, stub
}

func (s *stubRedis) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (s *stubRedis) ProcessHook(_ goredis.ProcessHook) goredis.ProcessHook {
	return func(_ context.Context, cmd goredis.Cmder) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.roundTrips++
		s.apply(cmd)
		return nil
	}
}

func (s *stubRedis) ProcessPipelineHook(_ goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return func(_ context.Context, cmds []goredis.Cmder) error {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.roundTrips++
		for _, cmd := range cmds {
			s.apply(cmd)
		}
		return nil
	}
}

func (s *stubRedis) apply(cmd goredis.Cmder) {
	args := cmd.Args()
	s.calls[cmd.Name()]++
	switch cmd.Name() {
	case "sadd":
		s.sadd(redisArg(args[1]), redisArg(args[2]))
	case "set":
		s.entries[redisArg(args[1])] = redisArg(args[2])
	case "smembers":
		s.replyMembers(cmd, redisArg(args[1]))
	case "mget":
		s.replyEntries(cmd, args[1:])
	case "get":
		s.replyEntry(cmd, redisArg(args[1]))
	}
}

func (s *stubRedis) sadd(key, member string) {
	if s.sets[key] == nil {
		s.sets[key] = map[string]bool{}
	}
	s.sets[key][member] = true
}

func (s *stubRedis) replyMembers(cmd goredis.Cmder, key string) {
	reply, ok := cmd.(*goredis.StringSliceCmd)
	if !ok {
		return
	}
	members := make([]string, 0, len(s.sets[key]))
	for member := range s.sets[key] {
		members = append(members, member)
	}
	reply.SetVal(members)
}

func (s *stubRedis) replyEntries(cmd goredis.Cmder, keys []any) {
	reply, ok := cmd.(*goredis.SliceCmd)
	if !ok {
		return
	}
	s.mgetKeys += len(keys)
	values := make([]any, len(keys))
	for i, key := range keys {
		if blob, stored := s.entries[redisArg(key)]; stored {
			values[i] = blob
		}
	}
	reply.SetVal(values)
}

func (s *stubRedis) replyEntry(cmd goredis.Cmder, key string) {
	reply, ok := cmd.(*goredis.StringCmd)
	if !ok {
		return
	}
	reply.SetVal(s.entries[key])
}

func (s *stubRedis) reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = map[string]int{}
	s.roundTrips = 0
	s.mgetKeys = 0
}

func redisArg(v any) string {
	switch arg := v.(type) {
	case string:
		return arg
	case []byte:
		return string(arg)
	}
	return fmt.Sprint(v)
}

// sharedTrigramVocabularySize is the scale the bound has to survive: one common
// trigram holding a large slice of a 50k-entry vocabulary.
const sharedTrigramVocabularySize = 20000

// firstThreeRunes stands in for the production metaphone at its worst: it
// buckets the whole seeded vocabulary under a single phonetic code.
func firstThreeRunes(term string) string {
	runes := []rune(term)
	if len(runes) < 3 {
		return term
	}
	return string(runes[:3])
}

func seedSharedTrigramVocabulary(t *testing.T, store *RedisVocabularyStore, target string) {
	t.Helper()
	entries := make([]domain.VocabularyEntry, 0, sharedTrigramVocabularySize+1)
	entries = append(entries, vocabTrack(target))
	for i := range sharedTrigramVocabularySize {
		entries = append(entries, vocabTrack(fmt.Sprintf("the%05d", i)))
	}
	for chunk := range slices.Chunk(entries, 2000) {
		if err := store.BulkAdd(context.Background(), chunk); err != nil {
			t.Fatalf("BulkAdd: %v", err)
		}
	}
}

func vocabTrack(norm string) domain.VocabularyEntry {
	return domain.VocabularyEntry{
		Term:       norm,
		TermNorm:   norm,
		Kind:       domain.VocabKindTrack,
		Popularity: 1,
	}
}

func TestFindClosest_CommonTrigramLoadsBoundedCandidatesInThreeRoundTrips(t *testing.T) {
	client, stub := newStubRedisClient(t)
	store := NewVocabularyStore(client, lowercaseNorm, WithMetaphone(firstThreeRunes))
	const target = "themesong"
	seedSharedTrigramVocabulary(t, store, target)
	stub.reset()

	start := time.Now()
	got, err := store.FindClosest(context.Background(), "themesonh", 5)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("FindClosest: %v", err)
	}
	if stub.roundTrips > 3 {
		t.Errorf("round trips = %d, want at most 3 (trigram pipeline, phonetic set, one MGET)", stub.roundTrips)
	}
	if stub.calls["get"] != 0 {
		t.Errorf("per-candidate GET = %d, want 0: survivors load in one MGET", stub.calls["get"])
	}
	loadCap := vocabFuzzyPrefilterCap + vocabPhoneticCandidateCap
	if stub.mgetKeys > loadCap {
		t.Errorf("entries loaded = %d of %d sharing the trigram, want at most %d",
			stub.mgetKeys, sharedTrigramVocabularySize, loadCap)
	}
	if elapsed > 2*time.Second {
		t.Errorf("lookup took %s over %d candidates, want a bounded lookup", elapsed, sharedTrigramVocabularySize)
	}
	if len(got) == 0 || got[0].TermNorm != target {
		t.Errorf("FindClosest = %+v, want %q first", got, target)
	}
}

func TestTrigramLookupKeys_ShortQueryKeepsEveryTrigram(t *testing.T) {
	got := trigramLookupKeys(trigrams("megaman"))
	if len(got) != 5 {
		t.Fatalf("lookup keys = %v, want all 5 trigrams of a short query", got)
	}
	if got[0] != vocabTriPrefix+"meg" {
		t.Errorf("first key = %q, want %q", got[0], vocabTriPrefix+"meg")
	}
}
