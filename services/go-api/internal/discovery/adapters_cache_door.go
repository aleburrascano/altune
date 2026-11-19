package discovery

import cache "altune/go-api/internal/discovery/internal/adapters/cache"

type (
	CacheOption       = cache.Option
	ProviderCacheTTLs = cache.ProviderCacheTTLs
	CacheSignal       = cache.Signal
)

var (
	NewRedisArtworkCache        = cache.NewRedisArtworkCache
	NewRedisEnrichmentCache     = cache.NewRedisEnrichmentCache
	NewRedisHeldSlateCache      = cache.NewRedisHeldSlateCache
	NewRedisIdentityStore       = cache.NewRedisIdentityStore
	NewRedisProviderResultCache = cache.NewRedisProviderResultCache
	NewRedisResultCache         = cache.NewRedisResultCache
	NewCacheSignal              = cache.NewSignal
	NewVocabularyStore          = cache.NewVocabularyStore
	WithMetaphone               = cache.WithMetaphone
	WithCacheSignal             = cache.WithSignal
	WithVocabSignal             = cache.WithVocabSignal
)
