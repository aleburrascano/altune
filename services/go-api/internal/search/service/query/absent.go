package query

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/search/service/artwork"
	"altune/go-api/internal/search/service/useractivity"
	"altune/go-api/internal/shared/ids"
	"context"
)

type NoRelated struct{}

func (NoRelated) Execute(context.Context, ids.UserId, []domain.SearchResult) []domain.RelatedGroup {
	return nil
}

type NoResultCache struct{}

func (NoResultCache) Get(context.Context, string) ([]domain.SearchResult, bool) { return nil, false }

func (NoResultCache) Set(context.Context, string, []domain.SearchResult) {}

type NoProviderResultCache struct{}

func (NoProviderResultCache) Get(context.Context, domain.ProviderName, string) ([]domain.SearchResult, bool) {
	return nil, false
}

func (NoProviderResultCache) Set(context.Context, domain.ProviderName, string, []domain.SearchResult) {
}

type NoEventStore struct{}

func (NoEventStore) Append(context.Context, domain.InteractionEvent) error { return nil }

type NoHistoryWriter struct{}

func (NoHistoryWriter) Insert(context.Context, *domain.SearchHistoryEntry) error { return nil }

func (NoHistoryWriter) TrimToN(context.Context, ids.UserId, int) error { return nil }

type NoFavorites struct{}

func (NoFavorites) Add(context.Context, ids.UserId, domain.Favorite) error { return nil }

func (NoFavorites) Remove(context.Context, ids.UserId, domain.ResultKind, string) error {
	return nil
}

func (NoFavorites) ListForUser(context.Context, ids.UserId) ([]domain.Favorite, error) {
	return nil, nil
}

type NoSpellingHints struct{}

func (NoSpellingHints) SuggestByPrefix(context.Context, string, int) ([]domain.SpellingHintsEntry, error) {
	return nil, nil
}

func (NoSpellingHints) FindClosest(context.Context, string, int) ([]domain.SpellingHintsEntry, error) {
	return nil, nil
}

func (NoSpellingHints) Add(context.Context, domain.SpellingHintsEntry) error { return nil }

func (NoSpellingHints) BulkAdd(context.Context, []domain.SpellingHintsEntry) error { return nil }

func (NoSpellingHints) Trim(context.Context, int) error { return nil }

func AbsentCollaborators() Collaborators {
	return Collaborators{
		Artwork:       artwork.NoFill{},
		Related:       NoRelated{},
		History:       NoHistoryWriter{},
		Vocabulary:    NoSpellingHints{},
		Events:        NoEventStore{},
		Activity:      useractivity.NoopActivityFeed{},
		Favorites:     NoFavorites{},
		ResultCache:   NoResultCache{},
		ProviderCache: NoProviderResultCache{},
		HeldSlates:    NoResultCache{},
	}
}
