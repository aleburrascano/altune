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
		mu              sync.Mutex
		perProvider     [][]domain.SearchResult
		statuses        []domain.ProviderSearchResponse
		wg              sync.WaitGroup
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
