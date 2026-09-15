package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"strings"
	"sync"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

// afterReadHook runs fn once, right after the first command named cmdName
// completes, so a concurrent write lands between Trim's read and its evict.
type afterReadHook struct {
	cmdName string
	once    sync.Once
	fn      func()
}

func (h *afterReadHook) DialHook(next goredis.DialHook) goredis.DialHook { return next }

func (h *afterReadHook) ProcessHook(next goredis.ProcessHook) goredis.ProcessHook {
	return func(ctx context.Context, cmd goredis.Cmder) error {
		err := next(ctx, cmd)
		if strings.EqualFold(cmd.Name(), h.cmdName) {
			h.once.Do(h.fn)
		}
		return err
	}
}

func (h *afterReadHook) ProcessPipelineHook(next goredis.ProcessPipelineHook) goredis.ProcessPipelineHook {
	return next
}

// TestVocabularyStore_Trim_ConcurrentReAddSurvives guards #1094: a term
// re-added with a higher popularity between Trim's overflow read and its
// evict must survive, and the index must stay within budget and in sync.
func TestVocabularyStore_Trim_ConcurrentReAddSurvives(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(ctx context.Context, s *RedisVocabularyStore, e domain.VocabularyEntry) error
	}{
		{"Add", func(ctx context.Context, s *RedisVocabularyStore, e domain.VocabularyEntry) error {
			return s.Add(ctx, e)
		}},
		{"BulkAdd", func(ctx context.Context, s *RedisVocabularyStore, e domain.VocabularyEntry) error {
			return s.BulkAdd(ctx, []domain.VocabularyEntry{e})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testTrimConcurrentReAdd(t, tc.write)
		})
	}
}

func testTrimConcurrentReAdd(
	t *testing.T,
	write func(ctx context.Context, s *RedisVocabularyStore, e domain.VocabularyEntry) error,
) {
	writerClient := testRedisClient(t)
	trimClient := testRedisClient(t)
	ctx := context.Background()

	victim, bumped, keeper := "qatrimracevictim", "qatrimracebumped", "qatrimracekeeper"
	vocabCleanKeys(t, victim, bumped, keeper)
	cleanKeys(t, writerClient, vocabMetaPrefix+"QATRIM")

	writer := NewVocabularyStore(writerClient, lowercaseNorm, WithMetaphone(qaTrimMetaphone))
	baseCount, err := writerClient.ZCard(ctx, vocabTermsKey).Result()
	if err != nil {
		t.Fatalf("ZCard: %v", err)
	}
	if err := writer.BulkAdd(ctx, []domain.VocabularyEntry{
		{Term: "QATrimRaceVictim", TermNorm: victim, Kind: "artist", Popularity: -3_000_000_000},
		{Term: "QATrimRaceBumped", TermNorm: bumped, Kind: "artist", Popularity: -2_000_000_000},
		{Term: "QATrimRaceKeeper", TermNorm: keeper, Kind: "artist", Popularity: -1_000_000_000},
	}); err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}

	// Overflow is 2 (victim, bumped). While Trim holds that stale read, the
	// bumped term is re-added above the keeper, so the true overflow becomes
	// (victim, keeper).
	reAdded := false
	trimClient.AddHook(&afterReadHook{cmdName: "zrange", fn: func() {
		reAdded = true
		if err := write(ctx, writer, domain.VocabularyEntry{
			Term: "QATrimRaceBumped", TermNorm: bumped, Kind: "artist", Popularity: -500_000_000,
		}); err != nil {
			t.Errorf("concurrent re-add: %v", err)
		}
	}})
	trimmer := NewVocabularyStore(trimClient, lowercaseNorm, WithMetaphone(qaTrimMetaphone))

	if err := trimmer.Trim(ctx, int(baseCount)+1); err != nil {
		t.Fatalf("Trim: %v", err)
	}
	if !reAdded {
		t.Fatal("hook never fired: the test no longer interleaves a write with Trim's read")
	}

	assertIndexed(t, writerClient, bumped, "QATrimRaceBumped", true)
	assertIndexed(t, writerClient, victim, "QATrimRaceVictim", false)
	assertIndexed(t, writerClient, keeper, "QATrimRaceKeeper", false)

	if score, err := writerClient.ZScore(ctx, vocabTermsKey, encodeMember(bumped, "QATrimRaceBumped", "artist")).Result(); err != nil || score != -500_000_000 {
		t.Errorf("bumped score = %v (err=%v), want the re-added popularity", score, err)
	}
	terms, err := writerClient.ZCard(ctx, vocabTermsKey).Result()
	if err != nil {
		t.Fatalf("ZCard terms: %v", err)
	}
	lex, err := writerClient.ZCard(ctx, vocabLexKey).Result()
	if err != nil {
		t.Fatalf("ZCard lex: %v", err)
	}
	if terms != baseCount+1 {
		t.Errorf("terms size = %d, want trimmed to budget %d", terms, baseCount+1)
	}
	if lex != terms {
		t.Errorf("lex size %d != terms size %d: index families out of sync", lex, terms)
	}
}

func assertIndexed(t *testing.T, client *goredis.Client, norm, term string, want bool) {
	t.Helper()
	ctx := context.Background()
	member := encodeMember(norm, term, "artist")

	_, err := client.ZScore(ctx, vocabTermsKey, member).Result()
	if got := err == nil; got != want {
		t.Errorf("%s in terms ZSET = %v (err=%v), want %v", norm, got, err, want)
	}
	_, err = client.ZScore(ctx, vocabLexKey, member).Result()
	if got := err == nil; got != want {
		t.Errorf("%s in lex ZSET = %v (err=%v), want %v", norm, got, err, want)
	}
	_, err = client.Get(ctx, vocabEntryPfx+norm).Result()
	if got := err == nil; got != want {
		t.Errorf("%s entry blob present = %v (err=%v), want %v", norm, got, err, want)
	}
	for _, tri := range trigrams(norm) {
		if got, _ := client.SIsMember(ctx, vocabTriPrefix+tri, norm).Result(); got != want {
			t.Errorf("%s in trigram set %q = %v, want %v", norm, tri, got, want)
		}
	}
	if got, _ := client.SIsMember(ctx, vocabMetaPrefix+"QATRIM", norm).Result(); got != want {
		t.Errorf("%s in metaphone set = %v, want %v", norm, got, want)
	}
}
