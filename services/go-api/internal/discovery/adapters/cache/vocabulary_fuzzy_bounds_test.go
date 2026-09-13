package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"strings"
	"sync"
	"testing"

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

func TestTrigramLookupKeys_ShortQueryKeepsEveryTrigram(t *testing.T) {
	got := trigramLookupKeys(trigrams("megaman"))
	if len(got) != 5 {
		t.Fatalf("lookup keys = %v, want all 5 trigrams of a short query", got)
	}
	if got[0] != vocabTriPrefix+"meg" {
		t.Errorf("first key = %q, want %q", got[0], vocabTriPrefix+"meg")
	}
}
