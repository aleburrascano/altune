package service

import (
	"context"
	"testing"

	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

func TestLibraryLensService_ArtistsRejectYearSort(t *testing.T) {
	svc := NewLibraryLensService(catalogtest.NewTrackRepo())

	_, err := svc.Artists(context.Background(), testUserId(), domain.LibraryQuery{Sort: domain.SortYear})

	var validation *domain.ValidationError
	if err == nil {
		t.Fatal("expected a validation error for sort=year on artists")
	}
	if !asValidation(err, &validation) {
		t.Fatalf("error = %v, want *domain.ValidationError", err)
	}
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

func svcAlbums(ctx context.Context, repo *catalogtest.TrackRepo, userId shared.UserId) ([]domain.AlbumGroup, error) {
	return NewLibraryLensService(repo).Albums(ctx, userId, domain.LibraryQuery{})
}

func asValidation(err error, target **domain.ValidationError) bool {
	v, ok := err.(*domain.ValidationError)
	if ok {
		*target = v
	}
	return ok
}
