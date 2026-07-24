package cache

import (
	"context"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

// qaTrimMetaphone buckets every qa-trim test term under one phonetic code so
// the metaphone key family participates in indexing and eviction.
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
	store := NewVocabularyStore(nil, lowercaseNorm)
	if err := store.Trim(context.Background(), 10); err != nil {
		t.Fatalf("nil-client Trim: %v, want nil", err)
	}
}

// Trim evicts the lowest-popularity overflow from EVERY key family (terms
// ZSET, lex ZSET, trigram sets, entry blob, metaphone set) and leaves the
// budgeted survivors fully queryable. The test terms carry hugely negative
// popularity so they are strictly the lowest-ranked members of the shared
// dev-Redis vocabulary — Trim can only ever evict them.
func TestVocabularyStore_Trim_EvictsAcrossAllKeyFamilies(t *testing.T) {
	client := testRedisClient(t)
	store := NewVocabularyStore(client, lowercaseNorm, WithMetaphone(qaTrimMetaphone))
	ctx := context.Background()

	victimA := "qatrimvictima"
	victimB := "qatrimvictimb"
	keeper := "qatrimkeeper"
	vocabCleanKeys(t, victimA, victimB, keeper)
	cleanKeys(t, client, vocabMetaPrefix+"QATRIM")

	baseCount, err := client.ZCard(ctx, vocabTermsKey).Result()
	if err != nil {
		t.Fatalf("ZCard: %v", err)
	}

	entries := []domain.VocabularyEntry{
		{Term: "QATrimVictimA", TermNorm: victimA, Kind: "artist", Popularity: -3_000_000_000},
		{Term: "QATrimVictimB", TermNorm: victimB, Kind: "artist", Popularity: -2_000_000_000},
		{Term: "QATrimKeeper", TermNorm: keeper, Kind: "artist", Popularity: -1_000_000_000},
	}
	if err := store.BulkAdd(ctx, entries); err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}

	// Budget = base + 1 → overflow of exactly 2 → the two lowest (the victims).
	if err := store.Trim(ctx, int(baseCount)+1); err != nil {
		t.Fatalf("Trim: %v", err)
	}

	for _, tc := range []struct {
		norm, term string
	}{{victimA, "QATrimVictimA"}, {victimB, "QATrimVictimB"}} {
		member := encodeMember(tc.norm, tc.term, "artist")
		if _, err := client.ZScore(ctx, vocabTermsKey, member).Result(); err != goredis.Nil {
			t.Errorf("%s still in terms ZSET (err=%v), want evicted", tc.norm, err)
		}
		if _, err := client.ZScore(ctx, vocabLexKey, member).Result(); err != goredis.Nil {
			t.Errorf("%s still in lex ZSET (err=%v), want evicted", tc.norm, err)
		}
		if _, err := client.Get(ctx, vocabEntryPfx+tc.norm).Result(); err != goredis.Nil {
			t.Errorf("%s entry blob still present (err=%v), want deleted", tc.norm, err)
		}
		for _, tri := range trigrams(tc.norm) {
			if isMember, _ := client.SIsMember(ctx, vocabTriPrefix+tri, tc.norm).Result(); isMember {
				t.Errorf("%s still in trigram set %q, want removed", tc.norm, tri)
			}
		}
		if isMember, _ := client.SIsMember(ctx, vocabMetaPrefix+"QATRIM", tc.norm).Result(); isMember {
			t.Errorf("%s still in metaphone set, want removed", tc.norm)
		}
	}

	// The keeper survives intact and stays queryable through the public path.
	results, err := store.SuggestByPrefix(ctx, "qatrimkeep", 10)
	if err != nil {
		t.Fatalf("SuggestByPrefix after trim: %v", err)
	}
	if len(results) != 1 || results[0].Term != "QATrimKeeper" {
		t.Errorf("keeper not suggestible after trim: %v", results)
	}
	if isMember, _ := client.SIsMember(ctx, vocabMetaPrefix+"QATRIM", keeper).Result(); !isMember {
		t.Error("keeper lost its metaphone index entry")
	}

	// Within budget → no-op: nothing else may disappear.
	if err := store.Trim(ctx, int(baseCount)+10); err != nil {
		t.Fatalf("Trim (within budget): %v", err)
	}
	if _, err := client.Get(ctx, vocabEntryPfx+keeper).Result(); err != nil {
		t.Errorf("within-budget Trim evicted the keeper: %v", err)
	}
}

// A phonetically-identical term with ZERO trigram overlap and a hopeless
// Levenshtein distance is reachable ONLY through the metaphone index — the
// exact case WithMetaphone exists for.
func TestVocabularyStore_WithMetaphone_PhoneticOnlyMatch(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()

	// "vvvv" vs query "wwww": no shared trigrams, lev distance 4 (> maxDist 1).
	vocabCleanKeys(t, "vvvv")
	cleanKeys(t, client, vocabMetaPrefix+"QAVW")

	plain := NewVocabularyStore(client, lowercaseNorm)
	phonetic := NewVocabularyStore(client, lowercaseNorm, WithMetaphone(qaTrimMetaphone))

	if err := phonetic.Add(ctx, domain.VocabularyEntry{
		Term: "vvvv", TermNorm: "vvvv", Kind: "artist", Popularity: 1,
	}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Without the phonetic index the term is unreachable.
	got, err := plain.FindClosest(ctx, "wwww", 5)
	if err != nil {
		t.Fatalf("FindClosest (plain): %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("plain store found %v — the fixture no longer isolates the phonetic path", got)
	}

	// With it, the phonetic bucket surfaces the term despite zero trigram
	// overlap and a failing Levenshtein filter.
	got, err = phonetic.FindClosest(ctx, "wwww", 5)
	if err != nil {
		t.Fatalf("FindClosest (phonetic): %v", err)
	}
	if len(got) != 1 || got[0].Term != "vvvv" {
		t.Errorf("phonetic FindClosest = %v, want the phonetically-equal term", got)
	}
}
