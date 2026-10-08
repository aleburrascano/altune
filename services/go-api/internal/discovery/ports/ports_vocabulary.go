package ports

import (
	"altune/go-api/internal/discovery/domain"
	"context"
)

type VocabularyReader interface {
	SuggestByPrefix(ctx context.Context, prefix string, limit int) ([]domain.VocabularyEntry, error)
	FindClosest(ctx context.Context, query string, limit int) ([]domain.VocabularyEntry, error)
}

type VocabularyWriter interface {
	Add(ctx context.Context, entry domain.VocabularyEntry) error
	BulkAdd(ctx context.Context, entries []domain.VocabularyEntry) error
	Trim(ctx context.Context, maxEntries int) error
}

type VocabularyStore interface {
	VocabularyReader
	VocabularyWriter
}
