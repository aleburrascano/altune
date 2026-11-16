package discovery

import cache "altune/go-api/internal/discovery/internal/adapters/cache"

type (
	CacheOption       = cache.Option
	ProviderCacheTTLs = cache.ProviderCacheTTLs
	CacheSignal       = cache.Signal
)

var (
	NewRedisArtworkCache          = cache.NewRedisArtworkCache
	NewRedisDeezerEnrichmentCache = cache.NewRedisDeezerEnrichmentCache
	NewRedisDeezerLyricsCache     = cache.NewRedisDeezerLyricsCache
	NewRedisEnrichmentCache       = cache.NewRedisEnrichmentCache
	NewRedisHeldSlateCache        = cache.NewRedisHeldSlateCache
	NewRedisIdentityStore         = cache.NewRedisIdentityStore
	NewRedisLastFmEnrichmentCache = cache.NewRedisLastFmEnrichmentCache
	NewRedisProviderResultCache   = cache.NewRedisProviderResultCache
	NewRedisResultCache           = cache.NewRedisResultCache
	NewRedisTrackerIndexCache     = cache.NewRedisTrackerIndexCache
	NewRedisUnreleasedCache       = cache.NewRedisUnreleasedCache
	NewCacheSignal                = cache.NewSignal
	NewVocabularyStore            = cache.NewVocabularyStore
	WithMetaphone                 = cache.WithMetaphone
	WithCacheSignal               = cache.WithSignal
	WithVocabSignal               = cache.WithVocabSignal
)
