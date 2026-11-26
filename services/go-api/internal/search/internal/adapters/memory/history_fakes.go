package memory

import (
	"altune/go-api/internal/search/domain"
	"altune/go-api/internal/shared/ids"
	"context"
)

type FakeHistoryWriter struct {
	InsertFn  func(ctx context.Context, entry *domain.SearchHistoryEntry) error
	TrimToNFn func(ctx context.Context, userId ids.UserId, n int) error
}

func (f *FakeHistoryWriter) Insert(ctx context.Context, entry *domain.SearchHistoryEntry) error {
	if f.InsertFn != nil {
		return f.InsertFn(ctx, entry)
	}
	return nil
}

func (f *FakeHistoryWriter) TrimToN(ctx context.Context, userId ids.UserId, n int) error {
	if f.TrimToNFn != nil {
		return f.TrimToNFn(ctx, userId, n)
	}
	return nil
}

type FakeHistoryReader struct {
	ListDistinctFn func(ctx context.Context, userId ids.UserId, limit int) ([]*domain.SearchHistoryEntry, error)
}

func (f *FakeHistoryReader) ListDistinctRecent(ctx context.Context, userId ids.UserId, limit int) ([]*domain.SearchHistoryEntry, error) {
	if f.ListDistinctFn != nil {
		return f.ListDistinctFn(ctx, userId, limit)
	}
	return nil, nil
}

type FakeHistoryEraser struct {
	EraseFn func(ctx context.Context, userId ids.UserId) error
}

func (f *FakeHistoryEraser) EraseSearchTextForUser(ctx context.Context, userId ids.UserId) error {
	if f.EraseFn != nil {
		return f.EraseFn(ctx, userId)
	}
	return nil
}
