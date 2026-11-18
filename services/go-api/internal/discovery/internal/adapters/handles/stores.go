package handles

import (
	"altune/go-api/internal/discovery/internal/adapters/cache"
	"altune/go-api/internal/discovery/internal/adapters/persistence"
	"altune/go-api/internal/discovery/internal/core/ports"
	"altune/go-api/internal/shared/phonetics"
	"altune/go-api/internal/shared/textnorm"
	"expvar"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	goredis "github.com/redis/go-redis/v9"
)

type Stores struct {
	Pool       *pgxpool.Pool
	Redis      *goredis.Client
	CacheOps   *expvar.Map
	Events     *persistence.PgxEventStore
	History    *persistence.PgxSearchHistoryRepository
	Vocabulary ports.VocabularyStore
}

func NewStores(d Deps) Stores {
	return Stores{
		Pool:       d.Pool,
		Redis:      d.Redis,
		CacheOps:   d.CacheOps,
		Events:     persistence.NewPgxEventStore(d.Pool),
		History:    persistence.NewPgxSearchHistoryRepository(d.Pool),
		Vocabulary: newVocabularyStore(d.Redis, d.CacheOps),
	}
}

func newVocabularyStore(redis *goredis.Client, cacheOps *expvar.Map) ports.VocabularyStore {
	if redis == nil {
		return nil
	}
	return cache.NewVocabularyStore(
		redis,
		textnorm.NormalizeForMatch,
		cache.WithMetaphone(phonetics.MetaphoneKey),
		cache.WithVocabSignal(cache.NewSignal(cacheOps, time.Now)),
	)
}

func (s Stores) CacheOption() cache.Option {
	return cache.WithSignal(cache.NewSignal(s.CacheOps, time.Now))
}

func (s Stores) IdentityStore() *cache.RedisIdentityStore {
	return cache.NewRedisIdentityStore(persistence.NewPgxIdentityStore(s.Pool), s.Redis, s.CacheOption())
}

func (s Stores) TrackerIndex() *cache.RedisTrackerIndexCache {
	return cache.NewRedisTrackerIndexCache(s.Redis, s.CacheOption())
}
