package artistpage_test

import (
	"altune/go-api/internal/search"
	"altune/go-api/internal/search/internal/searchtest"
	"context"
	"testing"
	"testing/synctest"
	"time"
)

func TestFanOutByIdentity_SlowProviderIsCutOffAtTheTenSecondDeadline(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		fast := &search.FakeArtistContentProvider{
			GetAlbumsFn: func(_ context.Context, _ search.ProviderName, _ string) ([]search.SearchResult, error) {
				return []search.SearchResult{search.V2Album(search.ProviderDeezer, "d-fl", "Fully Loaded", withDate("2026-04-01"))}, nil
			},
		}
		slow := &search.FakeArtistContentProvider{
			GetAlbumsFn: func(ctx context.Context, _ search.ProviderName, _ string) ([]search.SearchResult, error) {
				<-ctx.Done()
				return nil, ctx.Err()
			},
		}
		store := &search.FakeIdentityStore{Mbid: "mbid-che", Xref: map[search.ProviderKey]string{"deezer": "d1", "itunes": "i1"}}
		svc := searchtest.NewArtistContent(t, map[search.ProviderName]search.ArtistContentProvider{
			search.ProviderDeezer: fast,
			search.ProviderITunes: slow,
		}, searchtest.ArtistIdentities(store))

		start := time.Now()
		resp, err := svc.GetAlbums(context.Background(), search.ProviderDeezer, "d1", nil, 50)
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
