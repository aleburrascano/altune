package service

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/ports"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

type SearchMusicService struct {
	providers      []ports.SearchProvider
	queryCache     ports.QueryCache
	historyRepo    ports.SearchHistoryRepository
	circuitBreaker *CircuitBreaker
	qualityScorer  *QualityScorer
}

func NewSearchMusicService(
	providers []ports.SearchProvider,
	queryCache ports.QueryCache,
	historyRepo ports.SearchHistoryRepository,
	circuitBreaker *CircuitBreaker,
	qualityScorer *QualityScorer,
) *SearchMusicService {
	return &SearchMusicService{
		providers:      providers,
		queryCache:     queryCache,
		historyRepo:    historyRepo,
		circuitBreaker: circuitBreaker,
		qualityScorer:  qualityScorer,
	}
}

type SearchOutput struct {
	Results          []domain.SearchResult
	ProviderStatuses []domain.ProviderSearchResponse
}

func (s *SearchMusicService) Execute(ctx context.Context, userId shared.UserId, query *domain.SearchQuery) (*SearchOutput, error) {
	queryNorm := NormalizeForMatch(query.Raw)
	if query.QueryNorm == "" {
		query.QueryNorm = queryNorm
	}

	var (
		mu               sync.Mutex
		providerResults  []domain.ProviderSearchResponse
		wg               sync.WaitGroup
	)

	for _, provider := range s.providers {
		if !s.circuitBreaker.AllowRequest(provider.Name()) {
			mu.Lock()
			providerResults = append(providerResults, domain.ProviderSearchResponse{
				Provider: provider.Name(),
				Status:   domain.ProviderStatusCircuitOpen,
			})
			mu.Unlock()
			continue
		}

		wg.Add(1)
		go func(p ports.SearchProvider) {
			defer wg.Done()

			results, err := p.Search(ctx, query.Raw, query.Kinds)
			if err != nil {
				s.circuitBreaker.RecordFailure(p.Name())
				mu.Lock()
				providerResults = append(providerResults, domain.ProviderSearchResponse{
					Provider: p.Name(),
					Status:   domain.ProviderStatusError,
				})
				mu.Unlock()
				slog.WarnContext(ctx, "provider search failed",
					"provider", p.Name().String(), "error", err)
				return
			}

			s.circuitBreaker.RecordSuccess(p.Name())
			mu.Lock()
			providerResults = append(providerResults, domain.ProviderSearchResponse{
				Provider: p.Name(),
				Results:  results,
				Status:   domain.ProviderStatusOK,
			})
			mu.Unlock()
		}(provider)
	}

	wg.Wait()

	merged := FuseAndRank(providerResults, queryNorm, query.Limit)

	if s.historyRepo != nil {
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
		ProviderStatuses: providerResults,
	}, nil
}
