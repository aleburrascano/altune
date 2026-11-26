package setup

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/service/providerguard"
	"altune/go-api/internal/search/service/query"
	"altune/go-api/internal/search/service/rank"
	"altune/go-api/internal/search/service/related"
	"strings"
)

type searchOption struct {
	name   string
	option query.Option
}

func newSearchService(w wiring, breaker *providerguard.CircuitBreaker) *query.Service {
	named := append(unrecordedOptions(w, breaker), recordingOptions(w.deps, w.stores)...)
	return w.namedSearchService("discovery.search", named, breaker)
}

func newRankingOnlySearchService(w wiring) *query.Service {
	named := rankingOptions(w.deps.Search, w.stores)
	named = append(named, enrichmentCacheOptions(w.stores)...)
	named = append(named, vocabularyOptions(w.stores)...)
	return w.namedSearchService("discovery.ranking_only_search", named, providerguard.NewCircuitBreaker())
}

func (w wiring) namedSearchService(message string, named []searchOption, breaker *providerguard.CircuitBreaker) *query.Service {
	w.deps.Logger.InfoContext(w.deps.Lifecycle, message, "options", searchOptionNames(named))
	return query.NewService(w.deps.Tracker, w.table.searchProviders(), breaker, searchOptions(named)...)
}

func unrecordedOptions(w wiring, breaker *providerguard.CircuitBreaker) []searchOption {
	named := rankingOptions(w.deps.Search, w.stores)
	named = append(named, explorationAndDeadlineOptions(w.deps.Search)...)
	named = append(named, artworkAndRelatedOptions(w.table, w.stores, w.artwork, breaker)...)
	named = append(named, identityOptions(w.stores)...)
	named = append(named, resultCacheOptions(w.deps.Search, w.stores)...)
	named = append(named, enrichmentCacheOptions(w.stores)...)
	return append(named, vocabularyOptions(w.stores)...)
}

func recordingOptions(d connections.Deps, s connections.Stores) []searchOption {
	return append(eventOptions(d.Search, s), activityOptions(d.Activity)...)
}

func searchOptionNames(named []searchOption) string {
	names := make([]string, len(named))
	for i, o := range named {
		names[i] = o.name
	}
	return strings.Join(names, ",")
}

func searchOptions(named []searchOption) []query.Option {
	opts := make([]query.Option, len(named))
	for i, o := range named {
		opts[i] = o.option
	}
	return opts
}

func rankingOptions(settings connections.SearchSettings, s connections.Stores) []searchOption {
	named := []searchOption{{"history", query.WithHistoryRepository(s.History)}}
	if settings.TailDemotion {
		named = append(named, searchOption{"tail_demotion", query.WithTailDemotion()})
	}
	if settings.CrossKindProminence {
		named = append(named, searchOption{"cross_kind_prominence", query.WithCrossKindProminence()})
	}
	return named
}

func explorationAndDeadlineOptions(settings connections.SearchSettings) []searchOption {
	var named []searchOption
	if settings.Exploration {
		named = append(named, searchOption{"exploration", query.WithExploration(settings.ExplorationRate)})
	}
	return append(named, searchOption{"deadline", query.WithSearchDeadline(settings.Deadline)})
}

func artworkAndRelatedOptions(t *providerTable, s connections.Stores, artwork ports.TaggingArtworkLookup, breaker *providerguard.CircuitBreaker) []searchOption {
	similar := t.named(rowDeezer).similar
	findRelated := related.NewFindRelatedService(
		persistence.NewPostgresRelationshipQuerier(s.Pool), similar, similar,
		related.WithFindRelatedCircuitBreaker(breaker),
	)
	return []searchOption{
		{"artwork", query.WithArtworkLookup(artwork)},
		{"find_related", query.WithFindRelatedService(findRelated)},
	}
}

func identityOptions(s connections.Stores) []searchOption {
	var named []searchOption
	if s.Pool != nil {
		named = append(named, searchOption{"favorites", query.WithFavorites(persistence.NewPostgresFavoritesRepository(s.Pool))})
	}
	if s.Identities != nil {
		named = append(named, searchOption{"identity_store", query.WithIdentityStore(s.Identities)})
	}
	return named
}

func resultCacheOptions(settings connections.SearchSettings, s connections.Stores) []searchOption {
	if s.Redis == nil {
		return nil
	}
	return []searchOption{
		{"result_cache", query.WithResultCache(cache.NewRedisResultCache(s.Redis, s.CacheOption()))},
		{"provider_result_cache", query.WithProviderResultCache(cache.NewRedisProviderResultCache(s.Redis, settings.ProviderCacheTTL, s.CacheOption()))},
		{"held_slate_cache", query.WithHeldSlateCache(cache.NewRedisHeldSlateCache(s.Redis, s.CacheOption()))},
	}
}

func enrichmentCacheOptions(s connections.Stores) []searchOption {
	if s.Redis == nil {
		return nil
	}
	enrichmentCache := cache.NewRedisEnrichmentCache(s.Redis, s.CacheOption())
	return []searchOption{
		{"artwork_cache", query.WithArtworkCache(cache.NewRedisArtworkCache(s.Redis, s.CacheOption()))},
		{"mbid_index", query.WithMBIDIndex(enrichmentCache)},
	}
}

func vocabularyOptions(s connections.Stores) []searchOption {
	if s.SpellingHints == nil {
		return nil
	}
	return []searchOption{{"vocabulary", query.WithVocabularyStore(s.SpellingHints)}}
}

func eventOptions(settings connections.SearchSettings, s connections.Stores) []searchOption {
	named := []searchOption{{"event_store", query.WithEventStore(s.Events)}}
	if settings.BehavioralRanking {
		named = append(named, searchOption{"behavioral_ranking", query.WithBehavioralRanking(rank.NewSatisfactionConsumer(s.Events))})
	}
	return named
}

func activityOptions(activity ports.ActivityFeed) []searchOption {
	if activity == nil {
		return nil
	}
	return []searchOption{{"activity", query.WithSearchActivityFeed(activity)}}
}
