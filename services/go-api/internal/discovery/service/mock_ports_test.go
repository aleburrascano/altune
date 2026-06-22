package service

import (
	"context"

	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
)

// --- SearchClickRepository fake ---

type fakeSearchClickRepository struct {
	insertIfOutsideWindowFn func(ctx context.Context, click *domain.SearchClick, windowSeconds int) (bool, error)
}

func (f *fakeSearchClickRepository) InsertIfOutsideWindow(ctx context.Context, click *domain.SearchClick, windowSeconds int) (bool, error) {
	if f.insertIfOutsideWindowFn != nil {
		return f.insertIfOutsideWindowFn(ctx, click, windowSeconds)
	}
	return true, nil
}

// --- SearchHistoryRepository fake ---

type fakeSearchHistoryRepository struct {
	insertFn           func(ctx context.Context, entry *domain.SearchHistoryEntry) error
	trimToNFn          func(ctx context.Context, userId shared.UserId, n int) error
	listDistinctFn     func(ctx context.Context, userId shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error)
}

func (f *fakeSearchHistoryRepository) Insert(ctx context.Context, entry *domain.SearchHistoryEntry) error {
	if f.insertFn != nil {
		return f.insertFn(ctx, entry)
	}
	return nil
}

func (f *fakeSearchHistoryRepository) TrimToN(ctx context.Context, userId shared.UserId, n int) error {
	if f.trimToNFn != nil {
		return f.trimToNFn(ctx, userId, n)
	}
	return nil
}

func (f *fakeSearchHistoryRepository) ListDistinctRecent(ctx context.Context, userId shared.UserId, limit int) ([]*domain.SearchHistoryEntry, error) {
	if f.listDistinctFn != nil {
		return f.listDistinctFn(ctx, userId, limit)
	}
	return nil, nil
}

// --- AlbumContentProvider fake ---

type fakeAlbumContentProvider struct {
	getAlbumTracksFn func(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

func (f *fakeAlbumContentProvider) GetAlbumTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	if f.getAlbumTracksFn != nil {
		return f.getAlbumTracksFn(ctx, provider, externalID)
	}
	return nil, nil
}

// --- ArtistContentProvider fake ---

type fakeArtistContentProvider struct {
	getTopTracksFn func(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
	getAlbumsFn    func(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

func (f *fakeArtistContentProvider) GetArtistTopTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	if f.getTopTracksFn != nil {
		return f.getTopTracksFn(ctx, provider, externalID)
	}
	return nil, nil
}

func (f *fakeArtistContentProvider) GetArtistAlbums(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	if f.getAlbumsFn != nil {
		return f.getAlbumsFn(ctx, provider, externalID)
	}
	return nil, nil
}
