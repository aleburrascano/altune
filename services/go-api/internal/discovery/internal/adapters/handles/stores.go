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
	Identities ports.IdentityStore
}

func NewStores(d Deps) Stores {
	return Stores{
		Pool:       d.Pool,
		Redis:      d.Redis,
		CacheOps:   d.CacheOps,
		Events:     persistence.NewPgxEventStore(d.Pool),
		History:    persistence.NewPgxSearchHistoryRepository(d.Pool),
		Vocabulary: newVocabularyStore(d.Redis, d.CacheOps),
		Identities: identityStore(d),
	}
}

func identityStore(d Deps) ports.IdentityStore {
	if d.IdentityStore != nil {
		return d.IdentityStore
	}
	if d.Pool == nil {
		return nil
	}
	return cache.NewRedisIdentityStore(persistence.NewPgxIdentityStore(d.Pool), d.Redis, cacheOption(d.CacheOps))
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
	return cacheOption(s.CacheOps)
}

func cacheOption(cacheOps *expvar.Map) cache.Option {
	return cache.WithSignal(cache.NewSignal(cacheOps, time.Now))
}

func (s Stores) TrackerIndex() *cache.RedisTrackerIndexCache {
	return cache.NewRedisTrackerIndexCache(s.Redis, s.CacheOption())
}
