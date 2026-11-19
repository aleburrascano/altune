package content_test

import (
	"altune/go-api/internal/discovery"
	"context"
	"testing"
	"testing/synctest"
	"time"
)

func TestFanOutByIdentity_SlowProviderIsCutOffAtTheTenSecondDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fast := &discovery.FakeArtistContentProvider{
			GetAlbumsFn: func(_ context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
				return []discovery.SearchResult{discovery.V2Album(discovery.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
			},
		}
		slow := &discovery.FakeArtistContentProvider{
			GetAlbumsFn: func(ctx context.Context, _ discovery.ProviderName, _ string) ([]discovery.SearchResult, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}
		store := &discovery.FakeIdentityStore{Mbid: "mbid-che", Xref: map[discovery.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
		svc := discovery.NewGetArtistContentService(
			map[discovery.ProviderName]discovery.ArtistContentProvider{
				discovery.ProviderDeezer: fast,
				discovery.ProviderITunes: slow,
			},
			discovery.WithContentIdentityStore(store),
		)

		start := time.Now()
		resp, err := svc.GetAlbums(context.Background(), discovery.ProviderDeezer, "d1", nil, 50)
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
