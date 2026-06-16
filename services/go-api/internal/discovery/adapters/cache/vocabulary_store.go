package cache

import (
	"context"
	"encoding/json"
	"sort"
	"strings"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

const (
	vocabTermsKey   = "discovery:vocab:v1:terms"
	vocabTriPrefix  = "discovery:vocab:v1:tri:"
	vocabEntryPfx   = "discovery:vocab:v1:entry:"
	vocabMetaPrefix = "discovery:vocab:v1:meta:"
	memberSep       = "\x00"
)

// NormalizeFunc normalizes a term for matching. Injected to avoid
// import cycles between cache and service packages.
type NormalizeFunc func(string) string

// MetaphoneFunc computes a phonetic key for a term.
type MetaphoneFunc func(string) string

// RedisVocabularyStore implements ports.VocabularyStore backed by Redis
// sorted sets and trigram indexes.
type RedisVocabularyStore struct {
	client    *goredis.Client
	normalize NormalizeFunc
	metaphone MetaphoneFunc
}

func NewVocabularyStore(
	client *goredis.Client,
	normalize NormalizeFunc,
	opts ...VocabStoreOption,
) *RedisVocabularyStore {
	s := &RedisVocabularyStore{
		client:    client,
		normalize: normalize,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

type VocabStoreOption func(*RedisVocabularyStore)

func WithMetaphone(fn MetaphoneFunc) VocabStoreOption {
	return func(s *RedisVocabularyStore) { s.metaphone = fn }
}

// Add indexes a single vocabulary entry in Redis.
func (s *RedisVocabularyStore) Add(ctx context.Context, entry domain.VocabularyEntry) error {
	if s.client == nil {
		return nil
	}
	return s.indexEntry(ctx, entry)
}

// BulkAdd indexes multiple vocabulary entries using a pipeline.
func (s *RedisVocabularyStore) BulkAdd(ctx context.Context, entries []domain.VocabularyEntry) error {
	if s.client == nil || len(entries) == 0 {
		return nil
	}
	pipe := s.client.Pipeline()
	for _, e := range entries {
		addEntryToPipeline(pipe, ctx, s.buildNorm(e), e, s.metaphone)
	}
	_, err := pipe.Exec(ctx)
	return err
}

// SuggestByPrefix returns entries whose normalized term starts with prefix,
// sorted by popularity descending.
func (s *RedisVocabularyStore) SuggestByPrefix(
	ctx context.Context,
	prefix string,
	limit int,
) ([]domain.VocabularyEntry, error) {
	if s.client == nil {
		return nil, nil
	}
	return s.prefixSearch(ctx, prefix, limit)
}

// FindClosest returns entries whose trigram similarity to query is highest,
// filtered by Levenshtein distance.
func (s *RedisVocabularyStore) FindClosest(
	ctx context.Context,
	query string,
	limit int,
) ([]domain.VocabularyEntry, error) {
	if s.client == nil {
		return nil, nil
	}
	return s.fuzzySearch(ctx, query, limit)
}

// --- internal helpers -------------------------------------------------------

func (s *RedisVocabularyStore) buildNorm(e domain.VocabularyEntry) string {
	if e.TermNorm != "" {
		return e.TermNorm
	}
	if s.normalize != nil {
		return s.normalize(e.Term)
	}
	return strings.ToLower(e.Term)
}

func (s *RedisVocabularyStore) indexEntry(
	ctx context.Context,
	entry domain.VocabularyEntry,
) error {
	norm := s.buildNorm(entry)
	pipe := s.client.Pipeline()
	addEntryToPipeline(pipe, ctx, norm, entry, s.metaphone)
	_, err := pipe.Exec(ctx)
	return err
}

func addEntryToPipeline(
	pipe goredis.Pipeliner,
	ctx context.Context,
	norm string,
	entry domain.VocabularyEntry,
	metaphone MetaphoneFunc,
) {
	member := encodeMember(norm, entry.Term, entry.Kind)
	pipe.ZAdd(ctx, vocabTermsKey, goredis.Z{
		Score:  float64(entry.Popularity),
		Member: member,
	})
	entryJSON, _ := json.Marshal(vocabEntryData{
		Term:       entry.Term,
		Kind:       entry.Kind,
		Popularity: entry.Popularity,
	})
	pipe.Set(ctx, vocabEntryPfx+norm, entryJSON, 0)
	for _, tri := range trigrams(norm) {
		pipe.SAdd(ctx, vocabTriPrefix+tri, norm)
	}
	if metaphone != nil {
		code := metaphone(norm)
		if code != "" {
			pipe.SAdd(ctx, vocabMetaPrefix+code, norm)
		}
	}
}

// prefixSearch uses ZRANGEBYLEX for prefix matching, then re-sorts by score.
func (s *RedisVocabularyStore) prefixSearch(
	ctx context.Context,
	prefix string,
	limit int,
) ([]domain.VocabularyEntry, error) {
	norm := s.normalizeTerm(prefix)
	members, err := s.lexRangeMembers(ctx, norm)
	if err != nil {
		return nil, err
	}
	return s.membersToSortedEntries(members, limit), nil
}

func (s *RedisVocabularyStore) lexRangeMembers(
	ctx context.Context,
	normPrefix string,
) ([]string, error) {
	if normPrefix == "" {
		return s.topByScore(ctx)
	}
	min := "[" + normPrefix
	max := "[" + normPrefix + "\xff"
	return s.client.ZRangeByLex(ctx, vocabTermsKey, &goredis.ZRangeBy{
		Min: min,
		Max: max,
	}).Result()
}

func (s *RedisVocabularyStore) topByScore(ctx context.Context) ([]string, error) {
	return s.client.ZRevRangeByScore(ctx, vocabTermsKey, &goredis.ZRangeBy{
		Min:   "-inf",
		Max:   "+inf",
		Count: 100,
	}).Result()
}

func (s *RedisVocabularyStore) membersToSortedEntries(
	members []string,
	limit int,
) []domain.VocabularyEntry {
	entries := make([]domain.VocabularyEntry, 0, len(members))
	for _, m := range members {
		norm, term, kind := decodeMember(m)
		if norm == "" {
			continue
		}
		entries = append(entries, domain.VocabularyEntry{
			Term:     term,
			TermNorm: norm,
			Kind:     kind,
		})
	}
	s.attachScores(entries)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Popularity > entries[j].Popularity
	})
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

func (s *RedisVocabularyStore) attachScores(entries []domain.VocabularyEntry) {
	ctx := context.Background()
	for i := range entries {
		member := encodeMember(entries[i].TermNorm, entries[i].Term, entries[i].Kind)
		score, err := s.client.ZScore(ctx, vocabTermsKey, member).Result()
		if err == nil {
			entries[i].Popularity = int64(score)
		}
	}
}

// fuzzySearch retrieves candidates via trigram overlap and phonetic match,
// then scores by Jaccard coefficient with a phonetic confidence floor.
func (s *RedisVocabularyStore) fuzzySearch(
	ctx context.Context,
	query string,
	limit int,
) ([]domain.VocabularyEntry, error) {
	norm := s.normalizeTerm(query)
	queryTrigrams := trigrams(norm)
	if len(queryTrigrams) == 0 {
		return nil, nil
	}
	candidates, err := s.trigramCandidates(ctx, queryTrigrams)
	if err != nil {
		return nil, err
	}

	var phoneticSet map[string]bool
	if s.metaphone != nil {
		code := s.metaphone(norm)
		if code != "" {
			phoneticSet, _ = s.metaphoneCandidates(ctx, code)
			for norm := range phoneticSet {
				if _, exists := candidates[norm]; !exists {
					candidates[norm] = 0
				}
			}
		}
	}

	return s.scoreCandidatesWithPhonetic(ctx, candidates, queryTrigrams, norm, limit, phoneticSet)
}

func (s *RedisVocabularyStore) metaphoneCandidates(
	ctx context.Context,
	code string,
) (map[string]bool, error) {
	members, err := s.client.SMembers(ctx, vocabMetaPrefix+code).Result()
	if err != nil {
		return nil, err
	}
	result := make(map[string]bool, len(members))
	for _, m := range members {
		result[m] = true
	}
	return result, nil
}

func (s *RedisVocabularyStore) trigramCandidates(
	ctx context.Context,
	queryTrigrams []string,
) (map[string]int, error) {
	candidates := map[string]int{}
	for _, tri := range queryTrigrams {
		members, err := s.client.SMembers(ctx, vocabTriPrefix+tri).Result()
		if err != nil {
			continue
		}
		for _, m := range members {
			candidates[m]++
		}
	}
	return candidates, nil
}

type fuzzyCandidate struct {
	entry   domain.VocabularyEntry
	jaccard float64
}

func (s *RedisVocabularyStore) scoreCandidatesWithPhonetic(
	ctx context.Context,
	candidates map[string]int,
	queryTrigrams []string,
	queryNorm string,
	limit int,
	phoneticSet map[string]bool,
) ([]domain.VocabularyEntry, error) {
	scored := make([]fuzzyCandidate, 0, len(candidates))
	for norm, shared := range candidates {
		entry, err := s.loadEntry(ctx, norm)
		if err != nil {
			continue
		}
		candTrigrams := trigrams(norm)
		jaccard := jaccardCoefficient(shared, len(queryTrigrams), len(candTrigrams))

		// Phonetic matches get a confidence floor of 0.5
		if phoneticSet[norm] && jaccard < 0.5 {
			jaccard = 0.5
		}

		maxDist := maxLevenshtein(queryNorm)
		dist := levenshteinDist(queryNorm, norm)
		if dist > maxDist && !phoneticSet[norm] {
			continue
		}
		entry.TermNorm = norm
		scored = append(scored, fuzzyCandidate{entry: entry, jaccard: jaccard})
	}
	return topFuzzyCandidates(scored, limit), nil
}

func (s *RedisVocabularyStore) loadEntry(
	ctx context.Context,
	norm string,
) (domain.VocabularyEntry, error) {
	raw, err := s.client.Get(ctx, vocabEntryPfx+norm).Result()
	if err != nil {
		return domain.VocabularyEntry{}, err
	}
	var data vocabEntryData
	if err := json.Unmarshal([]byte(raw), &data); err != nil {
		return domain.VocabularyEntry{}, err
	}
	return domain.VocabularyEntry{
		Term:       data.Term,
		Kind:       data.Kind,
		Popularity: data.Popularity,
	}, nil
}

func topFuzzyCandidates(scored []fuzzyCandidate, limit int) []domain.VocabularyEntry {
	sort.Slice(scored, func(i, j int) bool {
		return scored[i].jaccard > scored[j].jaccard
	})
	if limit > 0 && len(scored) > limit {
		scored = scored[:limit]
	}
	results := make([]domain.VocabularyEntry, len(scored))
	for i, c := range scored {
		results[i] = c.entry
	}
	return results
}

func (s *RedisVocabularyStore) normalizeTerm(term string) string {
	if s.normalize != nil {
		return s.normalize(term)
	}
	return strings.ToLower(term)
}

// --- encoding / trigrams / Levenshtein --------------------------------------

type vocabEntryData struct {
	Term       string `json:"term"`
	Kind       string `json:"kind"`
	Popularity int64  `json:"popularity"`
}

func encodeMember(norm, term, kind string) string {
	return norm + memberSep + term + memberSep + kind
}

func decodeMember(member string) (norm, term, kind string) {
	parts := strings.SplitN(member, memberSep, 3)
	if len(parts) != 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

// trigrams decomposes a string into character trigrams.
// For strings shorter than 3 chars, the string itself is the only trigram.
func trigrams(s string) []string {
	if len(s) == 0 {
		return nil
	}
	if len(s) < 3 {
		return []string{s}
	}
	out := make([]string, 0, len(s)-2)
	for i := 0; i <= len(s)-3; i++ {
		out = append(out, s[i:i+3])
	}
	return out
}

func jaccardCoefficient(shared, totalA, totalB int) float64 {
	union := totalA + totalB - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}

func maxLevenshtein(query string) int {
	n := len(query)
	if n <= 4 {
		return 1
	}
	if n <= 8 {
		return 2
	}
	return 3
}

// levenshteinDist is a local copy to avoid importing the service package.
func levenshteinDist(s1, s2 string) int {
	if len(s1) == 0 {
		return len(s2)
	}
	if len(s2) == 0 {
		return len(s1)
	}
	prev := make([]int, len(s2)+1)
	curr := make([]int, len(s2)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(s1); i++ {
		curr[0] = i
		for j := 1; j <= len(s2); j++ {
			cost := 1
			if s1[i-1] == s2[j-1] {
				cost = 0
			}
			del := prev[j] + 1
			ins := curr[j-1] + 1
			sub := prev[j-1] + cost
			curr[j] = minInt(del, ins, sub)
		}
		prev, curr = curr, prev
	}
	return prev[len(s2)]
}

func minInt(a, b, c int) int {
	if b < a {
		a = b
	}
	if c < a {
		a = c
	}
	return a
}

// AllKeys returns all Redis keys used by a vocabulary entry (for test cleanup).
func AllKeys(norm string) []string {
	keys := []string{
		vocabTermsKey,
		vocabEntryPfx + norm,
	}
	for _, tri := range trigrams(norm) {
		keys = append(keys, vocabTriPrefix+tri)
	}
	return keys
}

// VocabTermsKeyForTest exposes the sorted set key for integration tests.
func VocabTermsKeyForTest() string { return vocabTermsKey }

// VocabEntryKeyForTest exposes the entry key prefix for integration tests.
func VocabEntryKeyForTest(norm string) string { return vocabEntryPfx + norm }

// VocabTriKeyForTest exposes the trigram key prefix for integration tests.
func VocabTriKeyForTest(tri string) string { return vocabTriPrefix + tri }
