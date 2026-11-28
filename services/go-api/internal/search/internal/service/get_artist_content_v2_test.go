package service_test

import (
	"altune/go-api/internal/search/internal/adapters/memory"
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"context"
	"testing"
	"testing/synctest"
	"time"
)

func TestFanOutByIdentity_SlowProviderIsCutOffAtTheTenSecondDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fast := &memory.ArtistContentProvider{
			GetAlbumsFn: func(_ context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
				return []domain.SearchResult{memory.V2Album(domain.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
			},
		}
		slow := &memory.ArtistContentProvider{
			GetAlbumsFn: func(ctx context.Context, _ domain.ProviderName, _ string) ([]domain.SearchResult, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}
		store := &memory.FakeIdentityStore{Mbid: "mbid-che", Xref: map[domain.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
		svc := memory.NewArtistContent(t, map[domain.ProviderName]ports.ArtistContentProvider{
			domain.ProviderDeezer: fast,
			domain.ProviderITunes: slow,
		}, memory.ArtistIdentities(store))

		start := time.Now()
		resp, err := svc.GetAlbums(context.Background(), domain.ProviderDeezer, "d1", nil, 50)
		if err != nil {
			t.Fatalf("GetAlbums error = %v", err)
		}
		if elapsed := time.Since(start); elapsed != 10*time.Second {
			t.Errorf("fan-out took %v, want the slow provider cut off at the 10s deadline", elapsed)
		}
		if len(resp.Items) != 1 || resp.Items[0].Title != "Fully Loaded" {
			t.Fatalf("items = %+v, want just the fast provider's album (slow one degraded)", resp.Items)
		}
	})
}
