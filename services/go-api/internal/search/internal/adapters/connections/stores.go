package connections

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/phonetics"
	"altune/go-api/internal/shared/textnorm"
	"expvar"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type Stores struct {
	Pool          *pgxpool.Pool
	Redis         *goredis.Client
	CacheOps      *expvar.Map
	Events        *persistence.PostgresEventStore
	History       *persistence.PostgresSearchHistoryRepository
	SpellingHints ports.SpellingHintsStore
	Identities    ports.IdentityStore
}

func NewStores(d Deps) (Stores, error) {
	var vocabulary ports.SpellingHintsStore
	if d.Redis != nil {
		store, err := newVocabularyStore(d.Redis, d.CacheOps)
		if err != nil {
			return Stores{}, err
		}
		vocabulary = store
	}
	return Stores{
		Pool:          d.Pool,
		Redis:         d.Redis,
		CacheOps:      d.CacheOps,
		Events:        persistence.NewPostgresEventStore(d.Pool),
		History:       persistence.NewPostgresSearchHistoryRepository(d.Pool),
		SpellingHints: vocabulary,
		Identities:    identityStore(d),
	}, nil
}

func identityStore(d Deps) ports.IdentityStore {
	if d.IdentityStore != nil {
		return d.IdentityStore
	}
	if d.Pool == nil {
		return nil
	}
	return cache.NewRedisIdentityStore(persistence.NewPostgresIdentityStore(d.Pool), d.Redis, cacheOption(d.CacheOps))
}

func newVocabularyStore(redis *goredis.Client, cacheOps *expvar.Map) (ports.SpellingHintsStore, error) {
	store, err := cache.NewVocabularyStore(
		redis,
		textnorm.NormalizeForMatch,
		phonetics.MetaphoneKey,
		cache.WithVocabSignal(cache.NewSignal(cacheOps, time.Now)),
	)
	if err != nil {
		return nil, fmt.Errorf("build the vocabulary store: %w", err)
	}
	return store, nil
}

func (s Stores) CacheOption() cache.Option {
	return cacheOption(s.CacheOps)
}

func cacheOption(cacheOps *expvar.Map) cache.Option {
	return cache.WithSignal(cache.NewSignal(cacheOps, time.Now))
}

func (s Stores) TrackerIndex() *cache.RedisTrackerIndexCache {
	return cache.NewRedisTrackerIndexCache(s.Redis, s.CacheOption())
}
