package library_test

import (
	"altune/go-api/internal/catalog"
	"altune/go-api/internal/catalog/catalogtest"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"strings"
	"testing"
)

func TestLibraryLensService_RejectsNegativeOffset(t *testing.T) {
	svc := catalog.NewLibraryLensService(catalogtest.NewTrackRepo())
	query := catalog.LibraryQuery{Offset: -1}

	cases := []struct {
		name string
		call func() error
	}{
		{"albums", func() error {
			_, err := svc.Albums(context.Background(), catalogtest.UserId(), query)
			return err
		}},
		{"artists", func() error {
			_, err := svc.Artists(context.Background(), catalogtest.UserId(), query)
			return err
		}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()

			if err == nil {
				t.Fatal("expected a validation error for a negative offset")
			}
			sharedtest.AssertValidationError(t, err)
			if !strings.Contains(err.Error(), "offset") {
				t.Fatalf("error = %q, want it to mention %q", err.Error(), "offset")
			}
		})
	}
}

func TestLibraryLensService_ArtistsRejectYearSort(t *testing.T) {
	svc := catalog.NewLibraryLensService(catalogtest.NewTrackRepo())

	_, err := svc.Artists(context.Background(), catalogtest.UserId(), catalog.LibraryQuery{Sort: catalog.SortYear})

	if err == nil {
		t.Fatal("expected a validation error for sort=year on artists")
	}
	validation := sharedtest.AssertValidationError(t, err)
	if validation.HTTPStatus() != 400 {
		t.Errorf("status = %d, want 400", validation.HTTPStatus())
	}
}

func TestLibraryLensService_ArtistsReportYearSortBeforeOffset(t *testing.T) {
	svc := catalog.NewLibraryLensService(catalogtest.NewTrackRepo())

	_, err := svc.Artists(context.Background(), catalogtest.UserId(), catalog.LibraryQuery{Sort: catalog.SortYear, Offset: -1})

	if err == nil {
		t.Fatal("expected a validation error")
	}
	if !strings.Contains(err.Error(), "year") {
		t.Errorf("error = %q, want the year-sort refusal to win over the offset one", err.Error())
	}
}

func TestLibraryLensService_GroupsOwnedTracks(t *testing.T) {
	ctx := context.Background()
	userId := catalogtest.UserId()
	repo := catalogtest.NewTrackRepo()
	catalogtest.SeedTrack(t, repo, userId, "One", "Metallica", "And Justice for All")
	catalogtest.SeedTrack(t, repo, userId, "Blackened", "Metallica", "And Justice for All")

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

	artists, err := catalog.NewLibraryLensService(repo).Artists(ctx, userId, catalog.LibraryQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(artists) != 1 || artists[0].TrackCount != 2 {
		t.Errorf("artists = %+v, want one artist with 2 tracks", artists)
	}
}

func TestLibraryLensService_ClampsLimit(t *testing.T) {
	ctx := context.Background()
	userId := catalogtest.UserId()

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
			svc := catalog.NewLibraryLensService(repo)

			if _, err := svc.Albums(ctx, userId, catalog.LibraryQuery{Limit: c.in}); err != nil {
				t.Fatalf("Albums: %v", err)
			}
			if repo.LastAlbumsQuery.Limit != c.want {
				t.Errorf("albums limit = %d, want %d", repo.LastAlbumsQuery.Limit, c.want)
			}

			if _, err := svc.Artists(ctx, userId, catalog.LibraryQuery{Limit: c.in}); err != nil {
				t.Fatalf("Artists: %v", err)
			}
			if repo.LastArtistsQuery.Limit != c.want {
				t.Errorf("artists limit = %d, want %d", repo.LastArtistsQuery.Limit, c.want)
			}
		})
	}
}

func svcAlbums(ctx context.Context, repo *catalogtest.TrackRepo, userId shared.UserId) ([]catalog.AlbumGroup, error) {
	return catalog.NewLibraryLensService(repo).Albums(ctx, userId, catalog.LibraryQuery{})
}
