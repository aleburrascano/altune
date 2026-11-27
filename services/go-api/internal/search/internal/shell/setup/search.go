package setup

import (
	"altune/go-api/internal/search/internal/adapters/cache"
	"altune/go-api/internal/search/internal/adapters/connections"
	"altune/go-api/internal/search/internal/adapters/persistence"
	"altune/go-api/internal/search/internal/core/ports"
	"altune/go-api/internal/search/internal/service/artwork"
	"altune/go-api/internal/search/internal/service/providerguard"
	"altune/go-api/internal/search/internal/service/query"
	"altune/go-api/internal/search/internal/service/rank"
	"altune/go-api/internal/search/internal/service/related"
	"altune/go-api/internal/shared/clock"
	"context"
	"fmt"
	"strings"
)

type searchPlan struct {
	collaborators query.Collaborators
	options       []query.Option
	chosen        []string
}

func newSearchPlan() *searchPlan {
	return &searchPlan{collaborators: query.AbsentCollaborators()}
}

func (p *searchPlan) choose(name string) {
	p.chosen = append(p.chosen, name)
}

func (p *searchPlan) option(name string, opt query.Option) {
	p.options = append(p.options, opt)
	p.choose(name)
}

func newSearchService(ctx context.Context, w wiring, breaker *providerguard.CircuitBreaker) (*query.Service, error) {
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

func newRankingOnlySearchService(ctx context.Context, w wiring) (*query.Service, error) {
	p := newSearchPlan()
	planRanking(p, w.deps.Search, w.stores)
	planVocabulary(p, w.stores)
	return w.namedSearchService(ctx, "discovery.ranking_only_search", p, providerguard.NewCircuitBreaker(clock.Now))
}

func (w wiring) namedSearchService(ctx context.Context, message string, p *searchPlan, breaker *providerguard.CircuitBreaker) (*query.Service, error) {
	w.deps.Logger.InfoContext(ctx, message, "options", strings.Join(p.chosen, ","))
	svc, err := query.NewService(w.deps.Tracker, w.table.searchProviders(), breaker, p.collaborators, p.options...)
	if err != nil {
		return nil, fmt.Errorf("build %s: %w", message, err)
	}
	return svc, nil
}

func planRanking(p *searchPlan, settings connections.SearchSettings, s connections.Stores) {
	p.collaborators.History = s.History
	p.choose("history")
	if settings.TailDemotion {
		p.option("tail_demotion", query.WithTailDemotion())
	}
	if settings.CrossKindProminence {
		p.option("cross_kind_prominence", query.WithCrossKindProminence())
	}
}

func planExplorationAndDeadline(p *searchPlan, settings connections.SearchSettings) {
	if settings.Exploration && settings.ExplorationRate > 0 {
		p.option("exploration", query.WithExploration(settings.ExplorationRate))
	}
	if settings.Deadline > 0 {
		p.option("deadline", query.WithSearchDeadline(settings.Deadline))
	}
}

func planArtworkAndRelated(p *searchPlan, w wiring, breaker *providerguard.CircuitBreaker) error {
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

func newFindRelatedService(t *providerTable, s connections.Stores, breaker *providerguard.CircuitBreaker) (*related.FindRelatedService, error) {
	similar := t.named(rowDeezer).similar
	library, err := related.LibraryMatches(persistence.NewPostgresRelationshipQuerier(s.Pool))
	if err != nil {
		return nil, fmt.Errorf("build the library matches source: %w", err)
	}
	albumTracks, err := related.AlbumTracksSource(similar, breaker)
	if err != nil {
		return nil, fmt.Errorf("build the album tracks source: %w", err)
	}
	artistAlbums, err := related.ArtistAlbumsSource(similar, breaker)
	if err != nil {
		return nil, fmt.Errorf("build the artist albums source: %w", err)
	}
	return related.NewFindRelatedService(clock.Now, library, albumTracks, artistAlbums)
}

func newArtworkFiller(w wiring) (*artwork.ArtworkFiller, error) {
	s := w.stores
	var identities ports.IdentityStore = artwork.NoIdentities{}
	if s.Identities != nil {
		identities = s.Identities
	}
	if s.Redis == nil {
		return artwork.NewPendingFiller(identities, w.deps.Tracker)
	}
	return artwork.NewArtworkFiller(
		w.artwork,
		cache.NewRedisArtworkCache(s.Redis, s.CacheOption()),
		identities,
		cache.NewRedisEnrichmentCache(s.Redis, s.CacheOption()),
		w.deps.Tracker,
	)
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
		scoring, err := rank.NewBehavioralScoring(rank.NewSatisfactionConsumer(w.stores.Events), clock.Now)
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
