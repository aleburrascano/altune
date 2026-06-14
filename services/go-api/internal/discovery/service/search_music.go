package service

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

const (
	enrichLimit       = 25
	enrichConcurrency = 8
)

type SearchMusicService struct {
	providers        []ports.SearchProvider
	queryCache       ports.QueryCache
	historyRepo      ports.SearchHistoryRepository
	circuitBreaker   *CircuitBreaker
	popularityResolver ports.PopularityResolver
	artworkResolver  ports.ArtworkResolver
	artworkCache     ports.ArtworkCache
	fanartResolver   ports.ArtworkResolver
	geniusResolver   ports.ArtworkResolver
}

func NewSearchMusicService(
	providers []ports.SearchProvider,
	queryCache ports.QueryCache,
	historyRepo ports.SearchHistoryRepository,
	circuitBreaker *CircuitBreaker,
) *SearchMusicService {
	return &SearchMusicService{
		providers:      providers,
		queryCache:     queryCache,
		historyRepo:    historyRepo,
		circuitBreaker: circuitBreaker,
	}
}

func (s *SearchMusicService) SetPopularityResolver(r ports.PopularityResolver) {
	s.popularityResolver = r
}

func (s *SearchMusicService) SetArtworkResolver(r ports.ArtworkResolver) {
	s.artworkResolver = r
}

func (s *SearchMusicService) SetArtworkCache(c ports.ArtworkCache) {
	s.artworkCache = c
}

func (s *SearchMusicService) SetFanartResolver(r ports.ArtworkResolver) {
	s.fanartResolver = r
}

func (s *SearchMusicService) SetGeniusResolver(r ports.ArtworkResolver) {
	s.geniusResolver = r
}

type SearchOutput struct {
	Results          []domain.SearchResult
	ProviderStatuses []domain.ProviderSearchResponse
	Partial          bool
}

func (s *SearchMusicService) Execute(ctx context.Context, userId shared.UserId, query *domain.SearchQuery, saveHistory bool) (*SearchOutput, error) {
	queryNorm := NormalizeForMatch(query.Raw)
	if query.QueryNorm == "" {
		query.QueryNorm = queryNorm
	}

	var (
		mu          sync.Mutex
		perProvider [][]domain.SearchResult
		statuses    []domain.ProviderSearchResponse
		wg          sync.WaitGroup
	)

	for _, provider := range s.providers {
		if !s.circuitBreaker.AllowRequest(provider.Name()) {
			mu.Lock()
			statuses = append(statuses, domain.ProviderSearchResponse{
				Provider: provider.Name(),
				Status:   domain.ProviderStatusCircuitOpen,
			})
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(p ports.SearchProvider) {
			defer wg.Done()

			provCtx, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
			defer cancel()

			results, err := p.Search(provCtx, query.Raw, query.Kinds)
			if err != nil {
				s.circuitBreaker.RecordFailure(p.Name())
				status := domain.ProviderStatusError
				if provCtx.Err() != nil {
					status = domain.ProviderStatusTimeout
				}
				mu.Lock()
				statuses = append(statuses, domain.ProviderSearchResponse{
					Provider: p.Name(),
					Status:   status,
				})
				mu.Unlock()
				slog.WarnContext(ctx, "provider search failed",
					"provider", p.Name().String(), "error", err)
				return
			}

			s.circuitBreaker.RecordSuccess(p.Name())
			mu.Lock()
			perProvider = append(perProvider, results)
			statuses = append(statuses, domain.ProviderSearchResponse{
				Provider: p.Name(),
				Results:  results,
				Status:   domain.ProviderStatusOK,
			})
			mu.Unlock()
		}(provider)
	}

	wg.Wait()

	merged := FuseAndRank(perProvider, queryNorm, nil)

	merged = s.enrich(ctx, merged)

	merged = Rerank(merged, queryNorm)

	if len(merged) > query.Limit {
		merged = merged[:query.Limit]
	}

	partial := false
	for _, st := range statuses {
		if st.Status != domain.ProviderStatusOK {
			partial = true
			break
		}
	}

	if saveHistory && s.historyRepo != nil {
		entry := &domain.SearchHistoryEntry{
			ID:         uuid.New(),
			UserId:     userId,
			Query:      query.Raw,
			QueryNorm:  queryNorm,
			ExecutedAt: time.Now().UTC(),
		}
		if err := s.historyRepo.Insert(ctx, entry); err != nil {
			slog.WarnContext(ctx, "failed to persist search history", "error", err)
		}
	}

	return &SearchOutput{
		Results:          merged,
		ProviderStatuses: statuses,
		Partial:          partial,
	}, nil
}

func (s *SearchMusicService) enrich(ctx context.Context, results []domain.SearchResult) []domain.SearchResult {
	if s.popularityResolver == nil && s.artworkResolver == nil && s.fanartResolver == nil && s.geniusResolver == nil {
		return results
	}

	limit := enrichLimit
	if len(results) < limit {
		limit = len(results)
	}
	if limit == 0 {
		return results
	}

	top := results[:limit]
	rest := results[limit:]

	sem := make(chan struct{}, enrichConcurrency)
	var wg sync.WaitGroup
	enriched := make([]domain.SearchResult, len(top))

	for i, r := range top {
		wg.Add(1)
		go func(idx int, result domain.SearchResult) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			enriched[idx] = s.enrichOne(ctx, result)
		}(i, r)
	}

	wg.Wait()
	return append(enriched, rest...)
}

const emptyArtHash = "d41d8cd98f00b204e9800998ecf8427e"

func (s *SearchMusicService) enrichOne(ctx context.Context, result domain.SearchResult) domain.SearchResult {
	extras := copyExtras(result.Extras)
	imageURL := result.ImageURL
	changed := false

	if s.popularityResolver != nil {
		pop, err := s.popularityResolver.GetPopularity(ctx, result.Title, result.Subtitle)
		if err == nil && pop > 0 {
			extras["popularity"] = pop
			changed = true
		} else if extras["popularity"] != nil {
			extras["popularity"] = 0.0
			changed = true
		}
	}

	needsArt := imageURL == "" || strings.Contains(imageURL, emptyArtHash)
	tryArt := needsArt || result.Kind == domain.ResultKindArtist

	mbid := getStringExtra(result, "mbid")

	if tryArt && s.artworkCache != nil {
		cachedURL, found, _ := s.artworkCache.Get(ctx, result.Kind, result.Title, result.Subtitle, mbid)
		if found {
			if cachedURL != "" {
				imageURL = cachedURL
				changed = true
			}
			tryArt = false
		}
	}

	var resolvedArt string

	if tryArt && s.fanartResolver != nil && mbid != "" {
		url, _ := s.fanartResolver.Resolve(ctx, result.Kind, result.Title, result.Subtitle, mbid)
		if url != "" {
			resolvedArt = url
			needsArt = false
		}
	}

	if needsArt && tryArt && s.geniusResolver != nil {
		url, _ := s.geniusResolver.Resolve(ctx, result.Kind, result.Title, result.Subtitle, mbid)
		if url != "" {
			resolvedArt = url
			needsArt = false
		}
	}

	if needsArt && tryArt && s.artworkResolver != nil {
		url, _ := s.artworkResolver.Resolve(ctx, result.Kind, result.Title, result.Subtitle, mbid)
		if url != "" {
			resolvedArt = url
		}
	}

	if resolvedArt != "" {
		imageURL = resolvedArt
		changed = true
	}

	if tryArt && s.artworkCache != nil {
		_ = s.artworkCache.Set(ctx, result.Kind, result.Title, result.Subtitle, mbid, resolvedArt)
	}

	if !changed {
		return result
	}

	result.ImageURL = imageURL
	result.Extras = extras
	return result
}
