package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"context"
)

type fakeHistoryWriter struct {
	insertFn  func(ctx context.Context, entry *domain.SearchHistoryEntry) error
	trimToNFn func(ctx context.Context, userId shared.UserId, n int) error
}

func (f *fakeHistoryWriter) Insert(ctx context.Context, entry *domain.SearchHistoryEntry) error {
	if f.insertFn != nil {
		return f.insertFn(ctx, entry)
	}
	return nil
}

func (f *fakeHistoryWriter) TrimToN(ctx context.Context, userId shared.UserId, n int) error {
	if f.trimToNFn != nil {
		return f.trimToNFn(ctx, userId, n)
	}
	return nil
}

type fakeHistoryReader struct {
	listDistinctFn func(ctx context.Context, userId shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error)
}

func (f *fakeHistoryReader) ListDistinctRecent(ctx context.Context, userId shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error) {
	if f.listDistinctFn != nil {
		return f.listDistinctFn(ctx, userId, limit)
	}
	return nil, nil
}

type fakeHistoryEraser struct {
	eraseFn func(ctx context.Context, userId shared.UserId) error
}

func (f *fakeHistoryEraser) EraseSearchTextForUser(ctx context.Context, userId shared.UserId) error {
	if f.eraseFn != nil {
		return f.eraseFn(ctx, userId)
	}
	return nil
}
