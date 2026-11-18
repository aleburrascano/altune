package content

import (
	"altune/go-api/internal/discovery/discoverytest"
	"altune/go-api/internal/discovery/domain"
	"altune/go-api/internal/discovery/internal/core/ports"
	"context"
	"testing"
	"time"
)

func withDate(d string) func(*domain.SearchResult) {
	return func(r *domain.SearchResult) { r.ReleaseDate = d }
}

func TestFanOutByIdentity_slowProviderCutOffAtTimeout(t *testing.T) {
	old := detailFanOutTimeout
	detailFanOutTimeout = 50 * time.Millisecond
	defer func() { detailFanOutTimeout = old }()

	fast := &discoverytest.ArtistContentProvider{
		GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			return []domain.SearchResult{discoverytest.V2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
		},
	}
	slow := &discoverytest.ArtistContentProvider{
		GetAlbumsFn: func(ctx context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	store := &discoverytest.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
	svc := NewGetArtistContentService(
		map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: fast,
			domain.ProviderITunes: slow,
		},
		WithContentIdentityStore(store),
	)

	start := time.Now()
	resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", nil, 50)
	if err != nil {
		t.Fatalf("GetAlbums error = %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("fan-out took %v, want the slow provider cut off near the 50ms timeout", elapsed)
	}
	if len(resp.Items) != 1 || resp.Items[0].Title != "Fully Loaded" {
		t.Fatalf("items = %+v, want just the fast provider's album (slow one degraded)", resp.Items)
	}
}
