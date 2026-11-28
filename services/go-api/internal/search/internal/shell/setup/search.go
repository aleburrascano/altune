package setup

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/search/internal/service"
	"altune/go-api/internal/shared/clock"
	"context"
	"fmt"
	"strings"
)

type searchPlan struct {
	collaborators service.Collaborators
	options       []service.Option
	chosen        []string
}

func newSearchPlan() *searchPlan {
	return &searchPlan{collaborators: service.AbsentCollaborators()}
}

func (p *searchPlan) choose(name string) {
	p.chosen = append(p.chosen, name)
}

func (p *searchPlan) option(name string, opt service.Option) {
	p.options = append(p.options, opt)
	p.choose(name)
}

func newSearchService(ctx context.Context, w wiring, breaker *service.CircuitBreaker) (*service.SearchService, error) {
	p := newSearchPlan()
	planRanking(p, w.deps.Search, w.stores)
	planExplorationAndDeadline(p, w.deps.Search)
	if err := planArtworkAndRelated(p, w, breaker); err != nil {
		return nil, err
	}
	planIdentityAndFavorites(p, w.stores)
	planResultCaches(p, w.deps.Search, w.stores)
	planEnrichmentCaches(p, w.stores)
	planVocabulary(p, w.stores)
	if err := planRecording(p, w); err != nil {
		return nil, err
	}
	return w.namedSearchService(ctx, "discovery.search", p, breaker)
}

func newRankingOnlySearchService(ctx context.Context, w wiring) (*service.SearchService, error) {
	p := newSearchPlan()
	planRanking(p, w.deps.Search, w.stores)
	planVocabulary(p, w.stores)
	return w.namedSearchService(ctx, "discovery.ranking_only_search", p, service.NewCircuitBreaker(clock.Now))
}

func (w wiring) namedSearchService(ctx context.Context, message string, p *searchPlan, breaker *service.CircuitBreaker) (*service.SearchService, error) {
	w.deps.Logger.InfoContext(ctx, message, "options", strings.Join(p.chosen, ","))
	options := append([]service.Option{service.WithProviderTimeouts(w.table.searchTimeouts())}, p.options...)
	svc, err := service.NewSearchService(w.deps.Tracker, w.table.searchProviders(), breaker, p.collaborators, options...)
	if err != nil {
		return nil, fmt.Errorf("build %s: %w", message, err)
	}
	return svc, nil
}

func planRanking(p *searchPlan, settings connections.SearchSettings, s connections.Stores) {
	p.collaborators.History = s.History
	p.choose("history")
	if settings.TailDemotion {
		p.option("tail_demotion", service.WithTailDemotion())
	}
	if settings.CrossKindProminence {
		p.option("cross_kind_prominence", service.WithCrossKindProminence())
	}
}

func planExplorationAndDeadline(p *searchPlan, settings connections.SearchSettings) {
	if settings.Exploration && settings.ExplorationRate > 0 {
		p.option("exploration", service.WithExploration(settings.ExplorationRate))
	}
	if settings.Deadline > 0 {
		p.option("deadline", service.WithSearchDeadline(settings.Deadline))
	}
}

func planArtworkAndRelated(p *searchPlan, w wiring, breaker *service.CircuitBreaker) error {
	filler, err := newArtworkFiller(w)
	if err != nil {
		return fmt.Errorf("build the artwork filler: %w", err)
	}
	p.collaborators.Artwork = filler
	p.choose("artwork")

	findRelated, err := newFindRelatedService(w.table, w.stores, breaker)
	if err != nil {
		return fmt.Errorf("build the find related service: %w", err)
	}
	p.collaborators.Related = findRelated
	p.choose("find_related")
	return nil
}

func newFindRelatedService(t *providerTable, s connections.Stores, breaker *service.CircuitBreaker) (*service.FindRelatedService, error) {
	similar := t.named(rowDeezer).similar
	library, err := service.LibraryMatches(persistence.NewPostgresRelationshipQuerier(s.Pool))
	if err != nil {
		return nil, fmt.Errorf("build the library matches source: %w", err)
	}
	albumTracks, err := service.AlbumTracksSource(similar, breaker)
	if err != nil {
		return nil, fmt.Errorf("build the album tracks source: %w", err)
	}
	artistAlbums, err := service.ArtistAlbumsSource(similar, breaker)
	if err != nil {
		return nil, fmt.Errorf("build the artist albums source: %w", err)
	}
	r0, err := service.NewFindRelatedService(clock.Now, library, albumTracks, artistAlbums)
	if err != nil {
		return r0, fmt.Errorf("new find related service: %w", err)
	}
	return r0, nil
}

func newArtworkFiller(w wiring) (*service.ArtworkFiller, error) {
	s := w.stores
	var identities ports.IdentityStore = service.NoIdentities{}
	if s.Identities != nil {
		identities = s.Identities
	}
	if s.Redis == nil {
		r0, err := service.NewPendingFiller(identities, w.deps.Tracker)
		if err != nil {
			return r0, fmt.Errorf("new pending filler: %w", err)
		}
		return r0, nil
	}
	r0, err := service.NewArtworkFiller(
		w.artwork,
		cache.NewRedisArtworkCache(s.Redis, s.CacheOption()),
		identities,
		cache.NewRedisEnrichmentCache(s.Redis, s.CacheOption()),
		w.deps.Tracker,
	)
	if err != nil {
		return r0, fmt.Errorf("new artwork filler: %w", err)
	}
	return r0, nil
}

func planIdentityAndFavorites(p *searchPlan, s connections.Stores) {
	if s.Pool != nil {
		p.collaborators.Favorites = persistence.NewPostgresFavoritesRepository(s.Pool)
		p.choose("favorites")
	}
	if s.Identities != nil {
		p.choose("identity_store")
	}
}

func planResultCaches(p *searchPlan, settings connections.SearchSettings, s connections.Stores) {
	if s.Redis == nil {
		return
	}
	p.collaborators.ResultCache = cache.NewRedisResultCache(s.Redis, s.CacheOption())
	p.choose("result_cache")
	p.collaborators.ProviderCache = cache.NewRedisProviderResultCache(s.Redis, settings.ProviderCacheTTL, s.CacheOption())
	p.choose("provider_result_cache")
	p.collaborators.HeldSlates = cache.NewRedisHeldSlateCache(s.Redis, s.CacheOption())
	p.choose("held_slate_cache")
}

func planEnrichmentCaches(p *searchPlan, s connections.Stores) {
	if s.Redis == nil {
		return
	}
	p.choose("artwork_cache")
	p.choose("mbid_index")
}

func planVocabulary(p *searchPlan, s connections.Stores) {
	if s.SpellingHints == nil {
		return
	}
	p.collaborators.Vocabulary = s.SpellingHints
	p.choose("vocabulary")
}

func planRecording(p *searchPlan, w wiring) error {
	p.collaborators.Events = w.stores.Events
	p.choose("event_store")
	if w.deps.Search.BehavioralRanking {
		scoring, err := service.NewBehavioralScoring(service.NewSatisfactionConsumer(w.stores.Events), clock.Now)
		if err != nil {
			return fmt.Errorf("build behavioral ranking: %w", err)
		}
		p.collaborators.Behavior = scoring
		p.choose("behavioral_ranking")
	}
	p.collaborators.Activity = w.deps.Activity
	p.choose("activity")
	return nil
}
