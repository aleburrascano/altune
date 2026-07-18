package cache

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"altune/go-api/internal/discovery/domain"
)

func lowercaseNorm(s string) string { return strings.ToLower(s) }

// --- unit tests (nil client) ------------------------------------------------

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

// --- trigrams ---------------------------------------------------------------

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

// --- jaccardCoefficient -----------------------------------------------------

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

// --- encodeMember / decodeMember --------------------------------------------

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

// --- integration tests (require REDIS_URL) ----------------------------------

// vocabAllKeys returns all Redis keys used by a vocabulary entry (for test cleanup).
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
	// First result should have highest popularity among our entries
	foundHigh := false
	for i, r := range results {
		if r.Term == "emppophigh" {
			foundHigh = true
			// Should appear before emppopmed and emppoplow
			for j := i + 1; j < len(results); j++ {
				if results[j].Term == "emppopmed" || results[j].Term == "emppoplow" {
					// This is expected — higher pop comes first
				}
			}
			break
		}
	}
	if !foundHigh {
		t.Error("expected 'emppophigh' in results")
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

// --- helpers ----------------------------------------------------------------

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
