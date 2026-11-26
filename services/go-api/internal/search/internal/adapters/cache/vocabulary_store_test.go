package cache_test

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/cache/spellingmatch"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
)

const (
	vocabTermsKey   = "discovery:vocab:v1:terms"
	vocabLexKey     = "discovery:vocab:v1:lex"
	vocabTriPrefix  = "discovery:vocab:v1:tri:"
	vocabEntryPfx   = "discovery:vocab:v1:entry:"
	vocabMetaPrefix = "discovery:vocab:v1:meta:"
)

func vocabMember(norm, term, kind string) string {
	return norm + "\x00" + term + "\x00" + kind
}

func lowercaseNorm(s string) string { return strings.ToLower(s) }

func TestVocabularyStore_NilClient_AddReturnsNil(t *testing.T) {
	store := newVocabularyStore(t, nil, lowercaseNorm, cache.NoMetaphone)
	err := store.Add(context.Background(), domain.SpellingHintsEntry{
		Term: "test", Kind: "artist", Popularity: 1,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestVocabularyStore_NilClient_BulkAddReturnsNil(t *testing.T) {
	store := newVocabularyStore(t, nil, lowercaseNorm, cache.NoMetaphone)
	err := store.BulkAdd(context.Background(), []domain.SpellingHintsEntry{
		{Term: "a", Kind: "track", Popularity: 1},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestVocabularyStore_NilClient_SuggestByPrefixReturnsNil(t *testing.T) {
	store := newVocabularyStore(t, nil, lowercaseNorm, cache.NoMetaphone)
	results, err := store.SuggestByPrefix(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results, got %v", results)
	}
}

func TestVocabularyStore_NilClient_FindClosestReturnsNil(t *testing.T) {
	store := newVocabularyStore(t, nil, lowercaseNorm, cache.NoMetaphone)
	results, err := store.FindClosest(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results, got %v", results)
	}
}

func TestVocabularyStore_Add_ThenSuggestByPrefix(t *testing.T) {
	_, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	ctx := context.Background()

	entry := domain.SpellingHintsEntry{
		Term: "Megaman", TermNorm: "megaman", Kind: "track", Popularity: 500,
	}
	if err := store.Add(ctx, entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	results, err := store.SuggestByPrefix(ctx, "mega", 10)
	if err != nil {
		t.Fatalf("SuggestByPrefix: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("SuggestByPrefix(mega) = %+v, want the one added entry", results)
	}
	if results[0] != entry {
		t.Errorf("SuggestByPrefix(mega)[0] = %+v, want %+v", results[0], entry)
	}
}

func TestVocabularyStore_Add_StoresTheMemberFormat(t *testing.T) {
	server, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)

	if err := store.Add(context.Background(), domain.SpellingHintsEntry{Term: "Megaman", Kind: "track", Popularity: 7}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	member := vocabMember("megaman", "Megaman", "track")
	if score, err := server.ZScore(vocabTermsKey, member); err != nil || score != 7 {
		t.Errorf("terms score of %q = %v (err %v), want 7", member, score, err)
	}
	if _, err := server.ZScore(vocabLexKey, member); err != nil {
		t.Errorf("lex has no %q: %v", member, err)
	}
}

func TestVocabularyStore_SuggestByPrefix_SkipsMalformedMembers(t *testing.T) {
	server, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	ctx := context.Background()
	if err := store.Add(ctx, domain.SpellingHintsEntry{Term: "Megaman", Kind: "track", Popularity: 1}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	for _, malformed := range []string{"megano-separators", "megatwo\x00parts"} {
		if _, err := server.ZAdd(vocabLexKey, 0, malformed); err != nil {
			t.Fatalf("seed %q: %v", malformed, err)
		}
	}

	results, err := store.SuggestByPrefix(ctx, "mega", 10)
	if err != nil {
		t.Fatalf("SuggestByPrefix: %v", err)
	}
	if len(results) != 1 || results[0].Term != "Megaman" {
		t.Errorf("SuggestByPrefix(mega) = %+v, want only Megaman", results)
	}
}

func TestVocabularyStore_FindClosest_FuzzyMatch(t *testing.T) {
	_, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	ctx := context.Background()

	entry := domain.SpellingHintsEntry{
		Term: "Megaman", TermNorm: "megaman", Kind: "track", Popularity: 500,
	}
	if err := store.Add(ctx, entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	results, err := store.FindClosest(ctx, "megamsn", 5)
	if err != nil {
		t.Fatalf("FindClosest: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected 'megaman' as fuzzy match for 'megamsn'")
	}
	got := results[0]
	if got.Term != "Megaman" || got.TermNorm != "megaman" || got.Kind != "track" || got.Popularity != 500 {
		t.Errorf("FindClosest(megamsn)[0] = %+v, want the stored Megaman entry", got)
	}
	if got.MatchScore <= 0 || got.MatchScore > 1 {
		t.Errorf("MatchScore = %v, want in (0, 1]", got.MatchScore)
	}
}

func TestVocabularyStore_FindClosest_SkipsEntriesWhoseBlobIsMissingOrCorrupt(t *testing.T) {
	server, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	ctx := context.Background()
	if err := store.BulkAdd(ctx, []domain.SpellingHintsEntry{
		{Term: "Megaman", Kind: "track", Popularity: 3},
		{Term: "Megamen", Kind: "track", Popularity: 2},
		{Term: "Megamon", Kind: "track", Popularity: 1},
	}); err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}
	server.Del(vocabEntryPfx + "megamen")
	if err := server.Set(vocabEntryPfx+"megamon", "{not json"); err != nil {
		t.Fatalf("corrupt megamon: %v", err)
	}

	results, err := store.FindClosest(ctx, "megaman", 5)
	if err != nil {
		t.Fatalf("FindClosest: %v", err)
	}
	if len(results) != 1 || results[0].TermNorm != "megaman" {
		t.Errorf("FindClosest(megaman) = %+v, want only the entry whose blob still decodes", results)
	}
}

func TestVocabularyStore_FindClosest_RanksByScoreAndHonoursTheLimit(t *testing.T) {
	_, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	ctx := context.Background()
	if err := store.BulkAdd(ctx, []domain.SpellingHintsEntry{
		{Term: "megamxx", Kind: "track"},
		{Term: "megaman", Kind: "track"},
		{Term: "megamax", Kind: "track"},
	}); err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}

	results, err := store.FindClosest(ctx, "megaman", 2)
	if err != nil {
		t.Fatalf("FindClosest: %v", err)
	}
	if len(results) != 2 || results[0].TermNorm != "megaman" || results[1].TermNorm != "megamax" {
		t.Fatalf("FindClosest(megaman, 2) = %+v, want megaman then megamax", results)
	}
	if results[0].MatchScore <= results[1].MatchScore {
		t.Errorf("scores %v then %v, want descending", results[0].MatchScore, results[1].MatchScore)
	}
}

func TestVocabularyStore_BulkAdd_AllRetrievable(t *testing.T) {
	_, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	ctx := context.Background()

	entries := make([]domain.SpellingHintsEntry, 100)
	for i := range entries {
		name := fmt.Sprintf("bulkartist%03d", i)
		entries[i] = domain.SpellingHintsEntry{
			Term:       name,
			TermNorm:   name,
			Kind:       "artist",
			Popularity: int64(100 - i),
		}
	}

	if err := store.BulkAdd(ctx, entries); err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}

	results, err := store.SuggestByPrefix(ctx, "bulkartist", 100)
	if err != nil {
		t.Fatalf("SuggestByPrefix: %v", err)
	}
	if len(results) != 100 {
		t.Errorf("expected 100 results, got %d", len(results))
	}
}

func TestVocabularyStore_EmptyPrefix_ReturnsByPopularity(t *testing.T) {
	_, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	ctx := context.Background()

	entries := []domain.SpellingHintsEntry{
		{Term: "emppoplow", TermNorm: "emppoplow", Kind: "track", Popularity: 10},
		{Term: "emppophigh", TermNorm: "emppophigh", Kind: "artist", Popularity: 999},
		{Term: "emppopmed", TermNorm: "emppopmed", Kind: "album", Popularity: 100},
	}
	if err := store.BulkAdd(ctx, entries); err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}

	results, err := store.SuggestByPrefix(ctx, "", 10)
	if err != nil {
		t.Fatalf("SuggestByPrefix empty: %v", err)
	}
	var got []string
	for _, r := range results {
		got = append(got, r.Term)
	}
	if strings.Join(got, ",") != "emppophigh,emppopmed,emppoplow" {
		t.Errorf("SuggestByPrefix(\"\") = %v, want emppophigh, emppopmed, emppoplow", got)
	}
}

func TestVocabularyStore_FindClosest_NoTrigrams_Empty(t *testing.T) {
	_, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)

	results, err := store.FindClosest(context.Background(), "", 5)
	if err != nil {
		t.Fatalf("FindClosest empty: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestVocabularyStore_PrefixResults_SortedByPopularity(t *testing.T) {
	_, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	ctx := context.Background()

	entries := []domain.SpellingHintsEntry{
		{Term: "sortpoptrack1", TermNorm: "sortpoptrack1", Kind: "track", Popularity: 50},
		{Term: "sortpoptrack2", TermNorm: "sortpoptrack2", Kind: "track", Popularity: 500},
		{Term: "sortpoptrack3", TermNorm: "sortpoptrack3", Kind: "track", Popularity: 200},
	}
	if err := store.BulkAdd(ctx, entries); err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}

	results, err := store.SuggestByPrefix(ctx, "sortpoptrack", 10)
	if err != nil {
		t.Fatalf("SuggestByPrefix: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}
	for i, want := range []domain.SpellingHintsEntry{entries[1], entries[2], entries[0]} {
		if results[i] != want {
			t.Errorf("result %d = %+v, want %+v", i, results[i], want)
		}
	}

	limited, err := store.SuggestByPrefix(ctx, "sortpoptrack", 2)
	if err != nil {
		t.Fatalf("SuggestByPrefix limit 2: %v", err)
	}
	if len(limited) != 2 || limited[0].Term != "sortpoptrack2" || limited[1].Term != "sortpoptrack3" {
		t.Errorf("SuggestByPrefix limit 2 = %+v, want the two most popular", limited)
	}
}

func qaTrimMetaphone(norm string) string {
	if strings.HasPrefix(norm, "qatrim") {
		return "QATRIM"
	}
	if norm == "vvvv" || norm == "wwww" {
		return "QAVW"
	}
	return ""
}

func TestVocabularyStore_NilClient_TrimReturnsNil(t *testing.T) {
	store := newVocabularyStore(t, nil, lowercaseNorm, cache.NoMetaphone)
	if err := store.Trim(context.Background(), 10); err != nil {
		t.Fatalf("nil-client Trim: %v, want nil", err)
	}
}

func TestVocabularyStore_Trim_EvictsAcrossAllKeyFamilies(t *testing.T) {
	_, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, qaTrimMetaphone)
	ctx := context.Background()

	victimA := "qatrimvictima"
	victimB := "qatrimvictimb"
	keeper := "qatrimkeeper"

	entries := []domain.SpellingHintsEntry{
		{Term: "QATrimVictimA", TermNorm: victimA, Kind: "artist", Popularity: -3_000_000_000},
		{Term: "QATrimVictimB", TermNorm: victimB, Kind: "artist", Popularity: -2_000_000_000},
		{Term: "QATrimKeeper", TermNorm: keeper, Kind: "artist", Popularity: -1_000_000_000},
	}
	if err := store.BulkAdd(ctx, entries); err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}

	if err := store.Trim(ctx, 1); err != nil {
		t.Fatalf("Trim: %v", err)
	}

	assertIndexed(t, client, victimA, "QATrimVictimA", false)
	assertIndexed(t, client, victimB, "QATrimVictimB", false)
	assertIndexed(t, client, keeper, "QATrimKeeper", true)

	results, err := store.SuggestByPrefix(ctx, "qatrimkeep", 10)
	if err != nil {
		t.Fatalf("SuggestByPrefix after trim: %v", err)
	}
	if len(results) != 1 || results[0].Term != "QATrimKeeper" {
		t.Errorf("keeper not suggestible after trim: %v", results)
	}

	if err := store.Trim(ctx, 10); err != nil {
		t.Fatalf("Trim (within budget): %v", err)
	}
	assertIndexed(t, client, keeper, "QATrimKeeper", true)
}

func TestVocabularyStore_Trim_NonPositiveBudgetKeepsEverything(t *testing.T) {
	_, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	ctx := context.Background()
	if err := store.Add(ctx, domain.SpellingHintsEntry{Term: "Keeper", Kind: "artist"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	for _, budget := range []int{0, -1} {
		if err := store.Trim(ctx, budget); err != nil {
			t.Fatalf("Trim(%d): %v", budget, err)
		}
	}
	assertIndexed(t, client, "keeper", "Keeper", true)
}

func TestVocabularyStore_Trim_SkipsMalformedMembers(t *testing.T) {
	server, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	ctx := context.Background()
	if _, err := server.ZAdd(vocabTermsKey, -1, "malformed"); err != nil {
		t.Fatalf("seed malformed: %v", err)
	}
	if err := store.Add(ctx, domain.SpellingHintsEntry{Term: "Keeper", Kind: "artist", Popularity: 5}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	if err := store.Trim(ctx, 1); err != nil {
		t.Fatalf("Trim: %v", err)
	}
	if _, err := server.ZScore(vocabTermsKey, "malformed"); err != nil {
		t.Errorf("malformed member was evicted (%v), want it left alone", err)
	}
	assertIndexed(t, client, "keeper", "Keeper", true)
}

func TestVocabularyStore_WithMetaphone_PhoneticOnlyMatch(t *testing.T) {
	_, client := newRedis(t)
	ctx := context.Background()

	plain := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	phonetic := newVocabularyStore(t, client, lowercaseNorm, qaTrimMetaphone)

	if err := phonetic.Add(ctx, domain.SpellingHintsEntry{
		Term: "vvvv", TermNorm: "vvvv", Kind: "artist", Popularity: 1,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := plain.FindClosest(ctx, "wwww", 5)
	if err != nil {
		t.Fatalf("FindClosest (plain): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("plain store found %v — the fixture no longer isolates the phonetic path", got)
	}

	got, err = phonetic.FindClosest(ctx, "wwww", 5)
	if err != nil {
		t.Fatalf("FindClosest (phonetic): %v", err)
	}
	if len(got) != 1 || got[0].Term != "vvvv" {
		t.Errorf("phonetic FindClosest = %v, want the phonetically-equal term", got)
	}
}

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

func TestVocabularyStore_Trim_ConcurrentReAddSurvives(t *testing.T) {
	for _, tc := range []struct {
		name  string
		write func(ctx context.Context, s *cache.RedisVocabularyStore, e domain.SpellingHintsEntry) error
	}{
		{"Add", func(ctx context.Context, s *cache.RedisVocabularyStore, e domain.SpellingHintsEntry) error {
			return s.Add(ctx, e)
		}},
		{"BulkAdd", func(ctx context.Context, s *cache.RedisVocabularyStore, e domain.SpellingHintsEntry) error {
			return s.BulkAdd(ctx, []domain.SpellingHintsEntry{e})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			testTrimConcurrentReAdd(t, tc.write)
		})
	}
}

func testTrimConcurrentReAdd(
	t *testing.T,
	write func(ctx context.Context, s *cache.RedisVocabularyStore, e domain.SpellingHintsEntry) error,
) {
	server, writerClient := newRedis(t)
	trimClient := secondClient(t, server)
	ctx := context.Background()

	victim, bumped, keeper := "qatrimracevictim", "qatrimracebumped", "qatrimracekeeper"

	writer := newVocabularyStore(t, writerClient, lowercaseNorm, qaTrimMetaphone)
	if err := writer.BulkAdd(ctx, []domain.SpellingHintsEntry{
		{Term: "QATrimRaceVictim", TermNorm: victim, Kind: "artist", Popularity: -3_000_000_000},
		{Term: "QATrimRaceBumped", TermNorm: bumped, Kind: "artist", Popularity: -2_000_000_000},
		{Term: "QATrimRaceKeeper", TermNorm: keeper, Kind: "artist", Popularity: -1_000_000_000},
	}); err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}

	reAdded := false
	trimClient.AddHook(&afterReadHook{cmdName: "zrange", fn: func() {
		reAdded = true
		if err := write(ctx, writer, domain.SpellingHintsEntry{
			Term: "QATrimRaceBumped", TermNorm: bumped, Kind: "artist", Popularity: -500_000_000,
		}); err != nil {
			t.Errorf("concurrent re-add: %v", err)
		}
	}})
	trimmer := newVocabularyStore(t, trimClient, lowercaseNorm, qaTrimMetaphone)

	if err := trimmer.Trim(ctx, 1); err != nil {
		t.Fatalf("Trim: %v", err)
	}
	if !reAdded {
		t.Fatal("hook never fired: the test no longer interleaves a write with Trim's read")
	}

	assertIndexed(t, writerClient, bumped, "QATrimRaceBumped", true)
	assertIndexed(t, writerClient, victim, "QATrimRaceVictim", false)
	assertIndexed(t, writerClient, keeper, "QATrimRaceKeeper", false)

	if score, err := writerClient.ZScore(ctx, vocabTermsKey, vocabMember(bumped, "QATrimRaceBumped", "artist")).Result(); err != nil || score != -500_000_000 {
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
	if terms != 1 {
		t.Errorf("terms size = %d, want trimmed to budget 1", terms)
	}
	if lex != terms {
		t.Errorf("lex size %d != terms size %d: index families out of sync", lex, terms)
	}
}

func newVocabularyStore(t testing.TB, client *goredis.Client, normalize cache.NormalizeFunc, metaphone cache.MetaphoneFunc, opts ...cache.VocabStoreOption) *cache.RedisVocabularyStore {
	t.Helper()
	store, err := cache.NewVocabularyStore(client, normalize, metaphone, opts...)
	if err != nil {
		t.Fatalf("cache.NewVocabularyStore: %v", err)
	}
	return store
}

func TestNewVocabularyStore_RefusesAMissingNormalizerOrMetaphone(t *testing.T) {
	tests := []struct {
		name      string
		normalize cache.NormalizeFunc
		metaphone cache.MetaphoneFunc
	}{
		{"normalizer", nil, cache.NoMetaphone},
		{"metaphone", strings.ToLower, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store, err := cache.NewVocabularyStore(nil, tt.normalize, tt.metaphone)

			if store != nil || !errors.Is(err, cache.ErrVocabularyCollaboratorRequired) {
				t.Fatalf("got (%v, %v), want a refusal wrapping ErrVocabularyCollaboratorRequired", store, err)
			}
		})
	}
}

func assertIndexed(t *testing.T, client *goredis.Client, norm, term string, want bool) {
	t.Helper()
	ctx := context.Background()
	member := vocabMember(norm, term, "artist")

	_, err := client.ZScore(ctx, vocabTermsKey, member).Result()
	if got := err == nil; got != want || (err != nil && !errors.Is(err, goredis.Nil)) {
		t.Errorf("%s in terms ZSET = %v (err=%v), want %v", norm, got, err, want)
	}
	_, err = client.ZScore(ctx, vocabLexKey, member).Result()
	if got := err == nil; got != want || (err != nil && !errors.Is(err, goredis.Nil)) {
		t.Errorf("%s in lex ZSET = %v (err=%v), want %v", norm, got, err, want)
	}
	_, err = client.Get(ctx, vocabEntryPfx+norm).Result()
	if got := err == nil; got != want || (err != nil && !errors.Is(err, goredis.Nil)) {
		t.Errorf("%s entry blob present = %v (err=%v), want %v", norm, got, err, want)
	}
	for _, tri := range spellingmatch.Trigrams(norm) {
		if got, _ := client.SIsMember(ctx, vocabTriPrefix+tri, norm).Result(); got != want {
			t.Errorf("%s in trigram set %q = %v, want %v", norm, tri, got, want)
		}
	}
	if code := qaTrimMetaphone(norm); code != "" {
		if got, _ := client.SIsMember(ctx, vocabMetaPrefix+code, norm).Result(); got != want {
			t.Errorf("%s in metaphone set = %v, want %v", norm, got, want)
		}
	}
}

func vocabStoredPopularity(t *testing.T, client *goredis.Client, norm string, member string) (float64, int64) {
	t.Helper()
	ctx := context.Background()
	score, err := client.ZScore(ctx, vocabTermsKey, member).Result()
	if err != nil {
		t.Fatalf("ZScore: %v", err)
	}
	raw, err := client.Get(ctx, vocabEntryPfx+norm).Bytes()
	if err != nil {
		t.Fatalf("Get entry: %v", err)
	}
	var data struct {
		Popularity int64 `json:"popularity"`
	}
	if err := json.Unmarshal(raw, &data); err != nil {
		t.Fatalf("unmarshal entry: %v", err)
	}
	return score, data.Popularity
}

func TestVocabularyStore_Add_DoesNotLowerChartPopularity(t *testing.T) {
	_, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	ctx := context.Background()
	member := vocabMember("popguard", "PopGuard", "artist")

	chart := domain.SpellingHintsEntry{Term: "PopGuard", Kind: "artist", Popularity: 500}
	if err := store.BulkAdd(ctx, []domain.SpellingHintsEntry{chart}); err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}
	searched := domain.SpellingHintsEntry{Term: "PopGuard", Kind: "artist", Popularity: 0}
	if err := store.Add(ctx, searched); err != nil {
		t.Fatalf("Add: %v", err)
	}
	score, pop := vocabStoredPopularity(t, client, "popguard", member)
	if score != 500 || pop != 500 {
		t.Errorf("after Add: score=%v entry popularity=%d, want 500/500", score, pop)
	}

	chart.Popularity = 100
	if err := store.BulkAdd(ctx, []domain.SpellingHintsEntry{chart}); err != nil {
		t.Fatalf("BulkAdd lower: %v", err)
	}
	score, pop = vocabStoredPopularity(t, client, "popguard", member)
	if score != 100 || pop != 100 {
		t.Errorf("after lower BulkAdd: score=%v entry popularity=%d, want 100/100", score, pop)
	}
}

func TestVocabularyStore_Add_NewMemberWithZeroPopularityIsAdded(t *testing.T) {
	_, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	ctx := context.Background()
	member := vocabMember("zeropop", "ZeroPop", "artist")

	if err := store.Add(ctx, domain.SpellingHintsEntry{Term: "ZeroPop", Kind: "artist"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	score, pop := vocabStoredPopularity(t, client, "zeropop", member)
	if score != 0 || pop != 0 {
		t.Errorf("score=%v entry popularity=%d, want 0/0", score, pop)
	}
}

func TestVocabularyStore_Add_SetsTheEntryTTL(t *testing.T) {
	server, client := newRedis(t)
	store := newVocabularyStore(t, client, lowercaseNorm, cache.NoMetaphone)
	if err := store.Add(context.Background(), domain.SpellingHintsEntry{Term: "Megaman", Kind: "track"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	assertStored(t, server, vocabEntryPfx+"megaman", 90*24*time.Hour)
}
