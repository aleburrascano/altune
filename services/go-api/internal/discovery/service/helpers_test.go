package service

import (
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/logging"
	"context"
	"log/slog"
	"testing"
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

type fakeAlbumContentProvider struct {
	getAlbumTracksFn func(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
}

func (f *fakeAlbumContentProvider) GetAlbumTracks(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error) {
	if f.getAlbumTracksFn != nil {
		return f.getAlbumTracksFn(ctx, provider, externalID)
	}
	return nil, nil
}

type fakeArtistContentProvider struct {
	getTopTracksFn func(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
	getAlbumsFn    func(ctx context.Context, provider domain.ProviderName, externalID string) ([]domain.SearchResult, error)
	resolveIDFn    func(ctx context.Context, name string) (string, bool)
}

func (f *fakeArtistContentProvider) ResolveArtistID(ctx context.Context, name string) (string, bool) {
	if f.resolveIDFn != nil {
		return f.resolveIDFn(ctx, name)
	}
	return "", false
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

// captureDetachedLogs installs the production handler chain and returns its
// ring. The bare JSON handler captureProductionLogs installs stamps no
// correlation id and applies no attr redaction, so it could not tell a line
// that reaches an operator from one the redaction filter drops. Stdout stays at
// Error so only the panic line is echoed into test output.
func captureDetachedLogs(t *testing.T) *logging.RingBuffer {
	t.Helper()
	prev := slog.Default()
	t.Cleanup(func() { slog.SetDefault(prev) })
	return logging.Setup("error", false)
}

func onlyRecord(t *testing.T, ring *logging.RingBuffer, msg string) logging.CapturedRecord {
	t.Helper()
	var found []logging.CapturedRecord
	for _, r := range ring.Snapshot() {
		if r.Message == msg {
			found = append(found, r)
		}
	}
	if len(found) != 1 {
		t.Fatalf("captured %d %q records, want exactly 1", len(found), msg)
	}
	return found[0]
}
