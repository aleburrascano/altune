package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"testing"
)

func TestLibraryLensService_ArtistsRejectYearSort(t *testing.T) {
	svc := NewLibraryLensService(catalogtest.NewTrackRepo())

	_, err := svc.Artists(context.Background(), testUserId(), domain.LibraryQuery{Sort: domain.SortYear})

	if err == nil {
		t.Fatal("expected a validation error for sort=year on artists")
	}
	validation := shared.AssertValidationError(t, err)
	if validation.HTTPStatus() != 400 {
		t.Errorf("status = %d, want 400", validation.HTTPStatus())
	}
}

func TestLibraryLensService_GroupsOwnedTracks(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	repo := catalogtest.NewTrackRepo()
	seedTrack(t, repo, userId, "One", "Metallica", "And Justice for All")
	seedTrack(t, repo, userId, "Blackened", "Metallica", "And Justice for All")

	albums, err := svcAlbums(ctx, repo, userId)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(albums) != 1 {
		t.Fatalf("albums = %d, want 1", len(albums))
	}
	if albums[0].TrackCount != 2 {
		t.Errorf("TrackCount = %d, want 2", albums[0].TrackCount)
	}

	artists, err := NewLibraryLensService(repo).Artists(ctx, userId, domain.LibraryQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(artists) != 1 || artists[0].TrackCount != 2 {
		t.Errorf("artists = %+v, want one artist with 2 tracks", artists)
	}
}

func TestLibraryLensService_ClampsLimit(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()

	cases := []struct {
		name string
		in   int
		want int
	}{
		{"zero defaults to 50", 0, 50},
		{"negative defaults to 50", -5, 50},
		{"in range passes through", 1, 1},
		{"over cap clamps to 2000", 9000, 2000},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			repo := catalogtest.NewTrackRepo()
			svc := NewLibraryLensService(repo)

			if _, err := svc.Albums(ctx, userId, domain.LibraryQuery{Limit: c.in}); err != nil {
				t.Fatalf("Albums: %v", err)
			}
			if repo.LastAlbumsQuery.Limit != c.want {
				t.Errorf("albums limit = %d, want %d", repo.LastAlbumsQuery.Limit, c.want)
			}

			if _, err := svc.Artists(ctx, userId, domain.LibraryQuery{Limit: c.in}); err != nil {
				t.Fatalf("Artists: %v", err)
			}
			if repo.LastArtistsQuery.Limit != c.want {
				t.Errorf("artists limit = %d, want %d", repo.LastArtistsQuery.Limit, c.want)
			}
		})
	}
}

func svcAlbums(ctx context.Context, repo *catalogtest.TrackRepo, userId shared.UserId) ([]domain.AlbumGroup, error) {
	return NewLibraryLensService(repo).Albums(ctx, userId, domain.LibraryQuery{})
}
