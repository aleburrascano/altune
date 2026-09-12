package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/uuid"
)

type featuringErrLister struct {
	ports.FeaturedArtistRepository
	err error
}

func (f featuringErrLister) ListTracksFeaturing(_ context.Context, _ shared.UserId, _ domain.FeaturedArtist) ([]*domain.Track, error) {
	return nil, f.err
}

func TestListFeaturingService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := testUserId()
	sza, _ := domain.NewFeaturedArtist("SZA", "", 0)
	drake, _ := domain.NewFeaturedArtist("Drake", "", 0)

	t.Run("returns only the tracks featuring the queried artist", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		withSza := seedTrack(t, repo, userId, "Feature", "Artist", "Album")
		withSza.FeaturedArtists = []domain.FeaturedArtist{sza}
		withDrake := seedTrack(t, repo, userId, "Other", "Artist", "Album")
		withDrake.FeaturedArtists = []domain.FeaturedArtist{drake}
		seedTrack(t, repo, userId, "Plain", "Artist", "Album")
		svc := NewListFeaturingService(repo)

		got, err := svc.Execute(ctx, userId, sza)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 {
			t.Fatalf("len = %d, want 1", len(got))
		}
		if got[0].ID != withSza.ID {
			t.Errorf("track = %v, want %v", got[0].ID, withSza.ID)
		}
	})

	t.Run("returns nothing when no track features the artist", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		withDrake := seedTrack(t, repo, userId, "Other", "Artist", "Album")
		withDrake.FeaturedArtists = []domain.FeaturedArtist{drake}
		svc := NewListFeaturingService(repo)

		got, err := svc.Execute(ctx, userId, sza)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})

	t.Run("excludes a matching track owned by another user", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		other := shared.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
		theirs := seedTrack(t, repo, other, "Feature", "Artist", "Album")
		theirs.FeaturedArtists = []domain.FeaturedArtist{sza}
		svc := NewListFeaturingService(repo)

		got, err := svc.Execute(ctx, userId, sza)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})

	t.Run("matches by identity key regardless of the queried display name", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		stored, _ := domain.NewFeaturedArtist("SZA", "mbid-1", 0)
		track := seedTrack(t, repo, userId, "Feature", "Artist", "Album")
		track.FeaturedArtists = []domain.FeaturedArtist{stored}
		svc := NewListFeaturingService(repo)

		query, _ := domain.NewFeaturedArtist("Solana", "mbid-1", 0)
		got, err := svc.Execute(ctx, userId, query)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got[0].ID != track.ID {
			t.Fatalf("got %d tracks, want the mbid-1 match", len(got))
		}
	})

	t.Run("repo error propagates", func(t *testing.T) {
		errRepo := errors.New("db error")
		svc := NewListFeaturingService(featuringErrLister{err: errRepo})

		_, err := svc.Execute(ctx, userId, sza)

		if !errors.Is(err, errRepo) {
			t.Fatalf("error = %v, want %v", err, errRepo)
		}
		if !strings.Contains(err.Error(), "list featuring") {
			t.Errorf("error = %q, want it to wrap with 'list featuring'", err.Error())
		}
	})
}
