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

// --- SearchProvider mock ---

type mockSearchProvider struct {
	name           domain.ProviderName
	supportedKinds map[domain.ResultKind]bool
	results        []domain.SearchResult
	err            error
	searchFn       func(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error)
}

func (m *mockSearchProvider) Name() domain.ProviderName { return m.name }

func (m *mockSearchProvider) SupportedKinds() map[domain.ResultKind]bool { return m.supportedKinds }

func (m *mockSearchProvider) Search(ctx context.Context, query string, kinds map[domain.ResultKind]bool) ([]domain.SearchResult, error) {
	if m.searchFn != nil {
		return m.searchFn(ctx, query, kinds)
	}
	return m.results, m.err
}

// --- PopularityResolver mock ---

type mockPopularityResolver struct {
	getPopularityFn func(ctx context.Context, title, artist string) (int64, error)
}

func (m *mockPopularityResolver) GetPopularity(ctx context.Context, title, artist string) (int64, error) {
	if m.getPopularityFn != nil {
		return m.getPopularityFn(ctx, title, artist)
	}
	return 0, nil
}

// --- ArtworkResolver mock ---

type mockArtworkResolver struct {
	resolveFn func(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error)
}

func (m *mockArtworkResolver) Resolve(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, error) {
	if m.resolveFn != nil {
		return m.resolveFn(ctx, kind, title, subtitle, mbid)
	}
	return "", nil
}

// --- ArtworkCache mock ---

type mockArtworkCache struct {
	getFn func(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, bool, error)
	setFn func(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid, url string) error
}

func (m *mockArtworkCache) Get(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid string) (string, bool, error) {
	if m.getFn != nil {
		return m.getFn(ctx, kind, title, subtitle, mbid)
	}
	return "", false, nil
}

func (m *mockArtworkCache) Set(ctx context.Context, kind domain.ResultKind, title, subtitle, mbid, url string) error {
	if m.setFn != nil {
		return m.setFn(ctx, kind, title, subtitle, mbid, url)
	}
	return nil
}
