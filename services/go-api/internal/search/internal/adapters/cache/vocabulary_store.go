package cache

import (
	"altune/go-api/internal/search/domain"
	"errors"
	"strings"
	"time"

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

var ErrVocabularyCollaboratorRequired = errors.New("vocabulary store needs its normalizer and metaphone, pass NoMetaphone to switch phonetic matching off")

func NoMetaphone(string) string { return "" }

func NewVocabularyStore(
	client *goredis.Client,
	normalize NormalizeFunc,
	metaphone MetaphoneFunc,
	opts ...VocabStoreOption,
) (*RedisVocabularyStore, error) {
	if normalize == nil || metaphone == nil {
		return nil, ErrVocabularyCollaboratorRequired
	}
	s := &RedisVocabularyStore{
		redisJSON: newRedisJSON(client, nil),
		normalize: normalize,
		metaphone: metaphone,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

type VocabStoreOption func(*RedisVocabularyStore)

func WithVocabSignal(sig *Signal) VocabStoreOption {
	return func(s *RedisVocabularyStore) { s.signal = sig }
}

func (s *RedisVocabularyStore) buildNorm(e domain.SpellingHintsEntry) string {
	if e.TermNorm != "" {
		return e.TermNorm
	}
	return s.normalize(e.Term)
}

func (s *RedisVocabularyStore) normalizeTerm(term string) string {
	return s.normalize(term)
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
