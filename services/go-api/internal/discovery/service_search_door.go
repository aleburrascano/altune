package discovery

import search "altune/go-api/internal/discovery/service/search"

type (
	SearchOption  = search.Option
	SearchOutput  = search.SearchOutput
	SearchService = search.Service
)

var (
	NewSearchService        = search.NewService
	WithArtworkCache        = search.WithArtworkCache
	WithArtworkResolver     = search.WithArtworkResolver
	WithBehavioralRanking   = search.WithBehavioralRanking
	WithCrossKindProminence = search.WithCrossKindProminence
	WithEventStore          = search.WithEventStore
	WithExploration         = search.WithExploration
	WithFavorites           = search.WithFavorites
	WithFindRelatedService  = search.WithFindRelatedService
	WithHeldSlateCache      = search.WithHeldSlateCache
	WithHistoryRepository   = search.WithHistoryRepository
	WithIdentityBridge      = search.WithIdentityBridge
	WithIdentityStore       = search.WithIdentityStore
	WithMBIDIndex           = search.WithMBIDIndex
	WithProviderResultCache = search.WithProviderResultCache
	WithResultCache         = search.WithResultCache
	WithSearchActivityFeed  = search.WithSearchActivityFeed
	WithSearchDeadline      = search.WithSearchDeadline
	WithTailDemotion        = search.WithTailDemotion
	WithVocabularyStore     = search.WithVocabularyStore
)
