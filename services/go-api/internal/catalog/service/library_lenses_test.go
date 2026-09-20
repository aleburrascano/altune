package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"strings"
	"testing"
)

func TestNormalizePage(t *testing.T) {
	cases := []struct {
		name      string
		limit     int
		offset    int
		wantLimit int
		wantErr   bool
	}{
		{name: "zero limit becomes the default page", limit: 0, wantLimit: 50},
		{name: "negative limit becomes the default page", limit: -5, wantLimit: 50},
		{name: "limit in range passes through", limit: 30, offset: 10, wantLimit: 30},
		{name: "limit at the cap passes through", limit: 2000, wantLimit: 2000},
		{name: "limit over the cap clamps to the cap", limit: 9000, wantLimit: 2000},
		{name: "negative offset is refused", limit: 30, offset: -1, wantErr: true},
		{name: "negative offset is refused before the limit is clamped", limit: 9000, offset: -3, wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			limit, err := normalizePage(c.limit, c.offset)

			if c.wantErr {
				if err == nil {
					t.Fatalf("expected a validation error, got limit = %d", limit)
				}
				sharedtest.AssertValidationError(t, err)
				if !strings.Contains(err.Error(), "offset") {
					t.Fatalf("error = %q, want it to mention %q", err.Error(), "offset")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if limit != c.wantLimit {
				t.Errorf("limit = %d, want %d", limit, c.wantLimit)
			}
		})
	}
}

func TestLibraryLensService_RejectsNegativeOffset(t *testing.T) {
	svc := NewLibraryLensService(catalogtest.NewTrackRepo())
	query := domain.LibraryQuery{Offset: -1}

	cases := []struct {
		name string
		call func() error
	}{
		{"albums", func() error {
			_, err := svc.Albums(context.Background(), testUserId(), query)
			return err
		}},
		{"artists", func() error {
			_, err := svc.Artists(context.Background(), testUserId(), query)
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
	svc := NewLibraryLensService(catalogtest.NewTrackRepo())

	_, err := svc.Artists(context.Background(), testUserId(), domain.LibraryQuery{Sort: domain.SortYear})

	if err == nil {
		t.Fatal("expected a validation error for sort=year on artists")
	}
	validation := sharedtest.AssertValidationError(t, err)
	if validation.HTTPStatus() != 400 {
		t.Errorf("status = %d, want 400", validation.HTTPStatus())
	}
}

func TestLibraryLensService_ArtistsReportYearSortBeforeOffset(t *testing.T) {
	svc := NewLibraryLensService(catalogtest.NewTrackRepo())

	_, err := svc.Artists(context.Background(), testUserId(), domain.LibraryQuery{Sort: domain.SortYear, Offset: -1})

	if err == nil {
		t.Fatal("expected a validation error")
	}
	if !strings.Contains(err.Error(), "year") {
		t.Errorf("error = %q, want the year-sort refusal to win over the offset one", err.Error())
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
