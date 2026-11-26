package search

import search "altune/go-api/internal/search/service/query"

type (
	SearchOption  = search.Option
	SearchOutput  = search.SearchOutput
	SearchService = search.Service
)

var (
	NewSearchService        = search.NewService
	WithArtworkCache        = search.WithArtworkCache
	WithArtworkLookup       = search.WithArtworkLookup
	WithBehavioralRanking   = search.WithBehavioralRanking
	WithCrossKindProminence = search.WithCrossKindProminence
	WithEventStore          = search.WithEventStore
	WithExploration         = search.WithExploration
	WithFavorites           = search.WithFavorites
	WithFindRelatedService  = search.WithFindRelatedService
	WithHeldSlateCache      = search.WithHeldSlateCache
	WithHistoryRepository   = search.WithHistoryRepository
	WithIdentityStore       = search.WithIdentityStore
	WithMBIDIndex           = search.WithMBIDIndex
	WithProviderResultCache = search.WithProviderResultCache
	WithResultCache         = search.WithResultCache
	WithSearchActivityFeed  = search.WithSearchActivityFeed
	WithSearchDeadline      = search.WithSearchDeadline
	WithTailDemotion        = search.WithTailDemotion
	WithVocabularyStore     = search.WithVocabularyStore
)
