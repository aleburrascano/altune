package cache

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared/textnorm"

	goredis "github.com/redis/go-redis/v9"
)

const (
	vocabTermsKey   = "discovery:vocab:v1:terms"
	// vocabLexKey is a parallel sorted set holding the same members all at score 0,
	// used only for ZRANGEBYLEX prefix queries. ZRANGEBYLEX is only well-defined
	// when every member shares one score; vocabTermsKey scores by popularity, so a
	// dedicated equal-score set is required for correct prefix matching.
	vocabLexKey     = "discovery:vocab:v1:lex"
	vocabTriPrefix  = "discovery:vocab:v1:tri:"
	vocabEntryPfx   = "discovery:vocab:v1:entry:"
	vocabMetaPrefix = "discovery:vocab:v1:meta:"
	memberSep       = "\x00"
	// vocabEntryTTL bounds the per-term JSON blobs (the largest per-term payload),
	// refreshed on every write so hot terms persist and cold ones reclaim. The
	// sorted-set + trigram index lifecycle is a separate (structural) concern.
	vocabEntryTTL = 90 * 24 * time.Hour
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

// Trim bounds the vocabulary to its maxEntries most popular terms, fully evicting
// the lowest-scored overflow across ALL five key families (terms ZSET, lex ZSET,
// trigram SETs, entry blobs, metaphone SETs). This is the store's owned retention
// — the single place that knows the multi-key model, so callers never reach in to
// prune it. A no-op when the store is nil-backed or already within budget.
func (s *RedisVocabularyStore) Trim(ctx context.Context, maxEntries int) error {
	if s.client == nil || maxEntries <= 0 {
		return nil
	}
	count, err := s.client.ZCard(ctx, vocabTermsKey).Result()
	if err != nil {
		return fmt.Errorf("vocab trim: card: %w", err)
	}
	overflow := int(count) - maxEntries
	if overflow <= 0 {
		return nil
	}
	// Lowest-scored members rank first (ascending) — the eviction set.
	members, err := s.client.ZRange(ctx, vocabTermsKey, 0, int64(overflow-1)).Result()
	if err != nil {
		return fmt.Errorf("vocab trim: range: %w", err)
	}
	pipe := s.client.Pipeline()
	for _, member := range members {
		norm, _, _ := decodeMember(member)
		if norm == "" {
			continue
		}
		s.queueEvict(ctx, pipe, norm, member)
	}
	if _, err := pipe.Exec(ctx); err != nil {
		return fmt.Errorf("vocab trim: evict: %w", err)
	}
	return nil
}

// queueEvict pipelines the removal of one term from every key family. member is
// the encoded sorted-set member (norm\x00term\x00kind); norm keys the per-trigram,
// metaphone, and blob entries. Sharing this keeps "what a vocabulary entry spans"
// in one place — adding a Delete(termNorm) later is one call to this.
func (s *RedisVocabularyStore) queueEvict(ctx context.Context, pipe goredis.Pipeliner, norm, member string) {
	pipe.ZRem(ctx, vocabTermsKey, member)
	pipe.ZRem(ctx, vocabLexKey, member)
	for _, tri := range trigrams(norm) {
		pipe.SRem(ctx, vocabTriPrefix+tri, norm)
	}
	if s.metaphone != nil {
		if code := s.metaphone(norm); code != "" {
			pipe.SRem(ctx, vocabMetaPrefix+code, norm)
		}
	}
	pipe.Del(ctx, vocabEntryPfx+norm)
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
	member := encodeMember(norm, entry.Term, string(entry.Kind))
	pipe.ZAdd(ctx, vocabTermsKey, goredis.Z{
		Score:  float64(entry.Popularity),
		Member: member,
	})
	// Mirror the member into the equal-score lex index for correct ZRANGEBYLEX.
	pipe.ZAdd(ctx, vocabLexKey, goredis.Z{Score: 0, Member: member})
	entryJSON, _ := json.Marshal(vocabEntryData{
		Term:       entry.Term,
		Kind:       string(entry.Kind),
		Popularity: entry.Popularity,
	})
	pipe.Set(ctx, vocabEntryPfx+norm, entryJSON, vocabEntryTTL)
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
	return s.membersToSortedEntries(ctx, members, limit), nil
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
	return s.client.ZRangeByLex(ctx, vocabLexKey, &goredis.ZRangeBy{
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
	ctx context.Context,
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
			Kind:     domain.VocabularyKind(kind),
		})
	}
	s.attachScores(ctx, entries)
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Popularity > entries[j].Popularity
	})
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries
}

func (s *RedisVocabularyStore) attachScores(ctx context.Context, entries []domain.VocabularyEntry) {
	for i := range entries {
		member := encodeMember(entries[i].TermNorm, entries[i].Term, string(entries[i].Kind))
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

		dist := textnorm.LevenshteinDistance(queryNorm, norm)
		maxDist := maxLevenshtein(queryNorm)
		if dist > maxDist && !phoneticSet[norm] {
			continue
		}

		maxLen := len([]rune(queryNorm))
		if cl := len([]rune(norm)); cl > maxLen {
			maxLen = cl
		}
		levSim := 0.0
		if maxLen > 0 {
			levSim = 1.0 - float64(dist)/float64(maxLen)
		}

		phonetic := 0.0
		if phoneticSet[norm] {
			phonetic = 1.0
		}

		qLen := len([]rune(queryNorm))
		cLen := len([]rune(norm))
		lengthSim := 0.0
		if qLen > 0 || cLen > 0 {
			bigger := qLen
			if cLen > bigger {
				bigger = cLen
			}
			diff := qLen - cLen
			if diff < 0 {
				diff = -diff
			}
			lengthSim = 1.0 - float64(diff)/float64(bigger)
		}

		combined := domain.VocabularyMatchScore(jaccard, levSim, phonetic, lengthSim)

		entry.TermNorm = norm
		scored = append(scored, fuzzyCandidate{entry: entry, jaccard: combined})
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
		Kind:       domain.VocabularyKind(data.Kind),
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
		c.entry.MatchScore = c.jaccard
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

// trigrams decomposes a string into character trigrams using rune indexing
// so multi-byte Unicode characters are handled correctly.
func trigrams(s string) []string {
	runes := []rune(s)
	if len(runes) == 0 {
		return nil
	}
	if len(runes) < 3 {
		return []string{s}
	}
	out := make([]string, 0, len(runes)-2)
	for i := 0; i <= len(runes)-3; i++ {
		out = append(out, string(runes[i:i+3]))
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
	n := len([]rune(query))
	if n <= 4 {
		return 1
	}
	if n <= 8 {
		return 2
	}
	return 3
}


