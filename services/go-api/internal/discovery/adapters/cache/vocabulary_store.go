package cache

import (
	"strings"
	"time"

	"altune/go-api/internal/discovery/domain"

	goredis "github.com/redis/go-redis/v9"
)

const (
	vocabTermsKey   = "discovery:vocab:v1:terms"
	vocabLexKey     = "discovery:vocab:v1:lex"
	vocabTriPrefix  = "discovery:vocab:v1:tri:"
	vocabEntryPfx   = "discovery:vocab:v1:entry:"
	vocabMetaPrefix = "discovery:vocab:v1:meta:"
	memberSep       = "\x00"
	vocabEntryTTL   = 90 * 24 * time.Hour
)

type NormalizeFunc func(string) string

type MetaphoneFunc func(string) string

type RedisVocabularyStore struct {
	redisJSON
	normalize NormalizeFunc
	metaphone MetaphoneFunc
}

func NewVocabularyStore(
	client *goredis.Client,
	normalize NormalizeFunc,
	opts ...VocabStoreOption,
) *RedisVocabularyStore {
	s := &RedisVocabularyStore{
		redisJSON: redisJSON{client: client},
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

func (s *RedisVocabularyStore) buildNorm(e domain.VocabularyEntry) string {
	if e.TermNorm != "" {
		return e.TermNorm
	}
	if s.normalize != nil {
		return s.normalize(e.Term)
	}
	return strings.ToLower(e.Term)
}

func (s *RedisVocabularyStore) normalizeTerm(term string) string {
	if s.normalize != nil {
		return s.normalize(term)
	}
	return strings.ToLower(term)
}

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
