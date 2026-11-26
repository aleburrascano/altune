package ports

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/shared/ids"
	"context"
)

type HistoryWriter interface {
	Insert(ctx context.Context, entry *domain.SearchHistoryEntry) error
	TrimToN(ctx context.Context, userId ids.UserId, n int) error
}

type HistoryReader interface {
	ListDistinctRecent(ctx context.Context, userId ids.UserId, limit int) ([]*domain.SearchHistoryEntry, error)
}

type HistoryEraser interface {
	EraseSearchTextForUser(ctx context.Context, userId ids.UserId) error
}

type DeletedUserDataEraser interface {
	EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error)
}
