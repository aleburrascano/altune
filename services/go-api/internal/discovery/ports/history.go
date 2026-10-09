package ports

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"context"
)

type HistoryWriter interface {
	Insert(ctx context.Context, entry *domain.SearchHistoryEntry) error
	TrimToN(ctx context.Context, userId shared.UserId, n int) error
}

type HistoryReader interface {
	ListDistinctRecent(ctx context.Context, userId shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error)
}

type HistoryEraser interface {
	EraseSearchTextForUser(ctx context.Context, userId shared.UserId) error
}

type DeletedIdentityEraser interface {
	EraseRowsOfDeletedIdentities(ctx context.Context) (int64, error)
}
