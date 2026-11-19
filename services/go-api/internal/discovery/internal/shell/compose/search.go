package compose

import (
	"altune/go-api/internal/discovery/internal/adapters/cache"
	"altune/go-api/internal/discovery/internal/adapters/handles"
	"altune/go-api/internal/discovery/internal/adapters/persistence"
	"altune/go-api/internal/discovery/internal/core/ports"
	"altune/go-api/internal/discovery/service/identity"
	"altune/go-api/internal/discovery/service/providercall"
	"altune/go-api/internal/discovery/service/rank"
	"altune/go-api/internal/discovery/service/related"
	"altune/go-api/internal/discovery/service/search"
	"strings"
)

type searchOption struct {
	name   string
	option search.Option
}

func newSearchService(w wiring, breaker *providercall.CircuitBreaker) *search.Service {
	named := append(unrecordedOptions(w, breaker), recordingOptions(w.deps, w.stores)...)
	return w.namedSearchService("discovery.search", named, breaker)
}

func newEvalSearchService(w wiring) *search.Service {
	breaker := providercall.NewCircuitBreaker()
	return w.namedSearchService("discovery.eval_search", unrecordedOptions(w, breaker), breaker)
}

func newRankingOnlySearchService(w wiring) *search.Service {
	named := rankingOptions(w.deps.Search, w.stores)
	named = append(named, enrichmentCacheOptions(w.stores)...)
	named = append(named, vocabularyOptions(w.stores)...)
	return w.namedSearchService("discovery.ranking_only_search", named, providercall.NewCircuitBreaker())
}

func (w wiring) namedSearchService(message string, named []searchOption, breaker *providercall.CircuitBreaker) *search.Service {
	w.deps.Logger.InfoContext(w.deps.Lifecycle, message, "options", searchOptionNames(named))
	return search.NewService(w.table.searchProviders(), breaker, searchOptions(named)...)
}

func unrecordedOptions(w wiring, breaker *providercall.CircuitBreaker) []searchOption {
	named := rankingOptions(w.deps.Search, w.stores)
	named = append(named, explorationAndDeadlineOptions(w.deps.Search)...)
	named = append(named, artworkAndRelatedOptions(w.table, w.stores, w.artwork, breaker)...)
	named = append(named, identityOptions(w.deps.Search, w.table, w.stores)...)
	named = append(named, resultCacheOptions(w.deps.Search, w.stores)...)
	named = append(named, enrichmentCacheOptions(w.stores)...)
	return append(named, vocabularyOptions(w.stores)...)
}

func recordingOptions(d handles.Deps, s handles.Stores) []searchOption {
	return append(eventOptions(d.Search, s), activityOptions(d.Activity)...)
}

func searchOptionNames(named []searchOption) string {
	names := make([]string, len(named))
	for i, o := range named {
		names[i] = o.name
	}
	return strings.Join(names, ",")
}

func searchOptions(named []searchOption) []search.Option {
	opts := make([]search.Option, len(named))
	for i, o := range named {
		opts[i] = o.option
	}
	return opts
}

func rankingOptions(settings handles.SearchSettings, s handles.Stores) []searchOption {
	named := []searchOption{{"history", search.WithHistoryRepository(s.History)}}
	if settings.TailDemotion {
		named = append(named, searchOption{"tail_demotion", search.WithTailDemotion()})
	}
	if settings.CrossKindProminence {
		named = append(named, searchOption{"cross_kind_prominence", search.WithCrossKindProminence()})
	}
	return named
}

func explorationAndDeadlineOptions(settings handles.SearchSettings) []searchOption {
	var named []searchOption
	if settings.Exploration {
		named = append(named, searchOption{"exploration", search.WithExploration(settings.ExplorationRate)})
	}
	return append(named, searchOption{"deadline", search.WithSearchDeadline(settings.Deadline)})
}

func artworkAndRelatedOptions(t *providerTable, s handles.Stores, artwork ports.TaggingArtworkResolver, breaker *providercall.CircuitBreaker) []searchOption {
	similar := t.named(rowDeezer).similar
	findRelated := related.NewFindRelatedService(
		persistence.NewPgxRelationshipQuerier(s.Pool), similar, similar,
		related.WithFindRelatedCircuitBreaker(breaker),
	)
	return []searchOption{
		{"artwork", search.WithArtworkResolver(artwork)},
		{"find_related", search.WithFindRelatedService(findRelated)},
	}
}

func identityOptions(settings handles.SearchSettings, t *providerTable, s handles.Stores) []searchOption {
	var named []searchOption
	if s.Pool != nil {
		named = append(named, searchOption{"favorites", search.WithFavorites(persistence.NewPgxFavoritesRepository(s.Pool))})
	}
	if s.Identities != nil {
		named = append(named, searchOption{"identity_store", search.WithIdentityStore(s.Identities)})
	}
	anchor := t.named(rowMusicBrainz).artistAnchor
	if settings.IdentityVerifyOnPersist && anchor != nil {
		verifier := identity.NewIdentityVerifier(anchor, t.identityVerifiers())
		named = append(named, searchOption{"identity_verifier", search.WithIdentityVerifier(verifier)})
	}
	return named
}

func resultCacheOptions(settings handles.SearchSettings, s handles.Stores) []searchOption {
	if s.Redis == nil {
		return nil
	}
	return []searchOption{
		{"result_cache", search.WithResultCache(cache.NewRedisResultCache(s.Redis, s.CacheOption()))},
		{"provider_result_cache", search.WithProviderResultCache(cache.NewRedisProviderResultCache(s.Redis, settings.ProviderCacheTTL, s.CacheOption()))},
		{"held_slate_cache", search.WithHeldSlateCache(cache.NewRedisHeldSlateCache(s.Redis, s.CacheOption()))},
	}
}

func enrichmentCacheOptions(s handles.Stores) []searchOption {
	if s.Redis == nil {
		return nil
	}
	enrichmentCache := cache.NewRedisEnrichmentCache(s.Redis, s.CacheOption())
	return []searchOption{
		{"artwork_cache", search.WithArtworkCache(cache.NewRedisArtworkCache(s.Redis, s.CacheOption()))},
		{"identity_bridge", search.WithIdentityBridge(enrichmentCache)},
		{"mbid_index", search.WithMBIDIndex(enrichmentCache)},
	}
}

func vocabularyOptions(s handles.Stores) []searchOption {
	if s.Vocabulary == nil {
		return nil
	}
	return []searchOption{{"vocabulary", search.WithVocabularyStore(s.Vocabulary)}}
}

func eventOptions(settings handles.SearchSettings, s handles.Stores) []searchOption {
	named := []searchOption{{"event_store", search.WithEventStore(s.Events)}}
	if settings.BehavioralRanking {
		named = append(named, searchOption{"behavioral_ranking", search.WithBehavioralRanking(rank.NewSatisfactionConsumer(s.Events))})
	}
	return named
}

func activityOptions(activity ports.ActivityFeed) []searchOption {
	if activity == nil {
		return nil
	}
	return []searchOption{{"activity", search.WithSearchActivityFeed(activity)}}
}
