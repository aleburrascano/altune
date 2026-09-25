package cache

import (
	"altune/go-api/internal/discovery/domain"
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	goredis "github.com/redis/go-redis/v9"
)

func lowercaseNorm(s string) string { return strings.ToLower(s) }

func TestVocabularyStore_NilClient_AddReturnsNil(t *testing.T) {
	store := NewVocabularyStore(nil, lowercaseNorm)
	err := store.Add(context.Background(), domain.VocabularyEntry{
		Term: "test", Kind: "artist", Popularity: 1,
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestVocabularyStore_NilClient_BulkAddReturnsNil(t *testing.T) {
	store := NewVocabularyStore(nil, lowercaseNorm)
	err := store.BulkAdd(context.Background(), []domain.VocabularyEntry{
		{Term: "a", Kind: "track", Popularity: 1},
	})
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}

func TestVocabularyStore_NilClient_SuggestByPrefixReturnsNil(t *testing.T) {
	store := NewVocabularyStore(nil, lowercaseNorm)
	results, err := store.SuggestByPrefix(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results, got %v", results)
	}
}

func TestVocabularyStore_NilClient_FindClosestReturnsNil(t *testing.T) {
	store := NewVocabularyStore(nil, lowercaseNorm)
	results, err := store.FindClosest(context.Background(), "test", 10)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if results != nil {
		t.Fatalf("expected nil results, got %v", results)
	}
}

func TestTrigrams(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{name: "empty", in: "", want: nil},
		{name: "one char", in: "a", want: []string{"a"}},
		{name: "two chars", in: "ab", want: []string{"ab"}},
		{name: "three chars", in: "abc", want: []string{"abc"}},
		{name: "megaman", in: "megaman", want: []string{"meg", "ega", "gam", "ama", "man"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := trigrams(tt.in)
			if !sliceEqual(got, tt.want) {
				t.Errorf("trigrams(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestJaccardCoefficient(t *testing.T) {
	tests := []struct {
		name             string
		shared, a, b     int
		wantMin, wantMax float64
	}{
		{"identical", 5, 5, 5, 0.99, 1.01},
		{"disjoint", 0, 3, 4, 0, 0.01},
		{"partial", 2, 3, 4, 0.39, 0.41},
		{"empty", 0, 0, 0, 0, 0.01},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := jaccardCoefficient(tt.shared, tt.a, tt.b)
			if got < tt.wantMin || got > tt.wantMax {
				t.Errorf("jaccard(%d,%d,%d) = %f, want [%f, %f]",
					tt.shared, tt.a, tt.b, got, tt.wantMin, tt.wantMax)
			}
		})
	}
}

func TestMemberEncoding(t *testing.T) {
	norm, term, kind := "megaman", "Megaman", "track"
	encoded := encodeMember(norm, term, kind)
	gotNorm, gotTerm, gotKind := decodeMember(encoded)
	if gotNorm != norm || gotTerm != term || gotKind != kind {
		t.Errorf("round-trip failed: got (%q, %q, %q)", gotNorm, gotTerm, gotKind)
	}
}

func TestDecodeMember_Invalid(t *testing.T) {
	norm, term, kind := decodeMember("no-separators")
	if norm != "" || term != "" || kind != "" {
		t.Errorf("expected empty on invalid member, got (%q, %q, %q)", norm, term, kind)
	}
}

func vocabAllKeys(norm string) []string {
	keys := []string{
		vocabTermsKey,
		vocabLexKey,
		vocabEntryPfx + norm,
	}
	for _, tri := range trigrams(norm) {
		keys = append(keys, vocabTriPrefix+tri)
	}
	return keys
}

func vocabCleanKeys(t *testing.T, norms ...string) {
	t.Helper()
	client := testRedisClient(t)
	ctx := context.Background()
	t.Cleanup(func() {
		for _, n := range norms {
			for _, k := range vocabAllKeys(n) {
				client.Del(ctx, k)
			}
		}
	})
}

func TestVocabularyStore_Add_ThenSuggestByPrefix(t *testing.T) {
	client := testRedisClient(t)
	store := NewVocabularyStore(client, lowercaseNorm)
	ctx := context.Background()

	entry := domain.VocabularyEntry{
		Term: "Megaman", TermNorm: "megaman", Kind: "track", Popularity: 500,
	}
	vocabCleanKeys(t, "megaman")

	if err := store.Add(ctx, entry); err != nil {
		t.Fatalf("Add: %v", err)
	}

	results, err := store.SuggestByPrefix(ctx, "mega", 10)
	if err != nil {
		t.Fatalf("SuggestByPrefix: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one result for prefix 'mega'")
	}
	if results[0].Term != "Megaman" {
		t.Errorf("expected term 'Megaman', got %q", results[0].Term)
	}
}

func TestVocabularyStore_FindClosest_FuzzyMatch(t *testing.T) {
	client := testRedisClient(t)
	store := NewVocabularyStore(client, lowercaseNorm)
	ctx := context.Background()

	entry := domain.VocabularyEntry{
		Term: "Megaman", TermNorm: "megaman", Kind: "track", Popularity: 500,
	}
	vocabCleanKeys(t, "megaman")

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
	if results[0].Term != "Megaman" {
		t.Errorf("expected term 'Megaman', got %q", results[0].Term)
	}
}

func TestVocabularyStore_BulkAdd_AllRetrievable(t *testing.T) {
	client := testRedisClient(t)
	store := NewVocabularyStore(client, lowercaseNorm)
	ctx := context.Background()

	entries := make([]domain.VocabularyEntry, 100)
	norms := make([]string, 100)
	for i := range entries {
		name := fmt.Sprintf("bulkartist%03d", i)
		entries[i] = domain.VocabularyEntry{
			Term:       name,
			TermNorm:   name,
			Kind:       "artist",
			Popularity: int64(100 - i),
		}
		norms[i] = name
	}
	vocabCleanKeys(t, norms...)

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
	client := testRedisClient(t)
	store := NewVocabularyStore(client, lowercaseNorm)
	ctx := context.Background()

	entries := []domain.VocabularyEntry{
		{Term: "emppoplow", TermNorm: "emppoplow", Kind: "track", Popularity: 10},
		{Term: "emppophigh", TermNorm: "emppophigh", Kind: "artist", Popularity: 999},
		{Term: "emppopmed", TermNorm: "emppopmed", Kind: "album", Popularity: 100},
	}
	norms := []string{"emppoplow", "emppophigh", "emppopmed"}
	vocabCleanKeys(t, norms...)

	if err := store.BulkAdd(ctx, entries); err != nil {
		t.Fatalf("BulkAdd: %v", err)
	}

	results, err := store.SuggestByPrefix(ctx, "", 10)
	if err != nil {
		t.Fatalf("SuggestByPrefix empty: %v", err)
	}
	if len(results) < 3 {
		t.Fatalf("expected at least 3 results, got %d", len(results))
	}
	rankOf := func(term string) int {
		for i, r := range results {
			if r.Term == term {
				return i
			}
		}
		return -1
	}
	high, med, low := rankOf("emppophigh"), rankOf("emppopmed"), rankOf("emppoplow")
	if high < 0 {
		t.Fatal("expected 'emppophigh' in results")
	}
	if med >= 0 && high > med {
		t.Errorf("emppophigh (pop 999) ranked after emppopmed (pop 100): %d > %d", high, med)
	}
	if low >= 0 && high > low {
		t.Errorf("emppophigh (pop 999) ranked after emppoplow (pop 10): %d > %d", high, low)
	}
}

func TestVocabularyStore_FindClosest_NoTrigrams_Empty(t *testing.T) {
	client := testRedisClient(t)
	store := NewVocabularyStore(client, lowercaseNorm)
	ctx := context.Background()

	results, err := store.FindClosest(ctx, "", 5)
	if err != nil {
		t.Fatalf("FindClosest empty: %v", err)
	}
	if len(results) != 0 {
		t.Errorf("expected empty results, got %d", len(results))
	}
}

func TestVocabularyStore_PrefixResults_SortedByPopularity(t *testing.T) {
	client := testRedisClient(t)
	store := NewVocabularyStore(client, lowercaseNorm)
	ctx := context.Background()

	entries := []domain.VocabularyEntry{
		{Term: "sortpoptrack1", TermNorm: "sortpoptrack1", Kind: "track", Popularity: 50},
		{Term: "sortpoptrack2", TermNorm: "sortpoptrack2", Kind: "track", Popularity: 500},
		{Term: "sortpoptrack3", TermNorm: "sortpoptrack3", Kind: "track", Popularity: 200},
	}
	norms := []string{"sortpoptrack1", "sortpoptrack2", "sortpoptrack3"}
	vocabCleanKeys(t, norms...)

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
	if results[0].Term != "sortpoptrack2" {
		t.Errorf("expected first result 'sortpoptrack2' (pop 500), got %q (pop %d)",
			results[0].Term, results[0].Popularity)
	}
	if results[1].Term != "sortpoptrack3" {
		t.Errorf("expected second result 'sortpoptrack3' (pop 200), got %q (pop %d)",
			results[1].Term, results[1].Popularity)
	}
	if results[2].Term != "sortpoptrack1" {
		t.Errorf("expected third result 'sortpoptrack1' (pop 50), got %q (pop %d)",
			results[2].Term, results[2].Popularity)
	}
}

func sliceEqual(a, b []string) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
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
	store := NewVocabularyStore(nil, lowercaseNorm)
	if err := store.Trim(context.Background(), 10); err != nil {
		t.Fatalf("nil-client Trim: %v, want nil", err)
	}
}

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

	if err := store.Trim(ctx, int(baseCount)+1); err != nil {
		t.Fatalf("Trim: %v", err)
	}

	for _, tc := range []struct {
		norm, term string
	}{{victimA, "QATrimVictimA"}, {victimB, "QATrimVictimB"}} {
		member := encodeMember(tc.norm, tc.term, "artist")
		if _, err := client.ZScore(ctx, vocabTermsKey, member).Result(); !errors.Is(err, goredis.Nil) {
			t.Errorf("%s still in terms ZSET (err=%v), want evicted", tc.norm, err)
		}
		if _, err := client.ZScore(ctx, vocabLexKey, member).Result(); !errors.Is(err, goredis.Nil) {
			t.Errorf("%s still in lex ZSET (err=%v), want evicted", tc.norm, err)
		}
		if _, err := client.Get(ctx, vocabEntryPfx+tc.norm).Result(); !errors.Is(err, goredis.Nil) {
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

	if err := store.Trim(ctx, int(baseCount)+10); err != nil {
		t.Fatalf("Trim (within budget): %v", err)
	}
	if _, err := client.Get(ctx, vocabEntryPfx+keeper).Result(); err != nil {
		t.Errorf("within-budget Trim evicted the keeper: %v", err)
	}
}

func TestVocabularyStore_WithMetaphone_PhoneticOnlyMatch(t *testing.T) {
	client := testRedisClient(t)
	ctx := context.Background()

	vocabCleanKeys(t, "vvvv")
	cleanKeys(t, client, vocabMetaPrefix+"QAVW")

	plain := NewVocabularyStore(client, lowercaseNorm)
	phonetic := NewVocabularyStore(client, lowercaseNorm, WithMetaphone(qaTrimMetaphone))

	if err := phonetic.Add(ctx, domain.VocabularyEntry{
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
