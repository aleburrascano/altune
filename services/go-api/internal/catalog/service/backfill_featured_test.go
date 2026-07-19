package service

import (
	"context"
	"errors"
	"testing"

	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"

	"github.com/google/uuid"
)

type fakeResolver struct {
	byTitle map[string][]domain.FeaturedArtist
	err     error
}

func (f fakeResolver) Resolve(_ context.Context, _, title string) ([]domain.FeaturedArtist, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.byTitle[title], nil
}

func newTrackFeat(t *testing.T, userId shared.UserId, title string) *domain.Track {
	t.Helper()
	track, err := domain.NewTrack(userId, title, "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	return track
}

func TestBackfillFeaturedService(t *testing.T) {
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Run("resolves and persists, idempotent", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		t1 := newTrackFeat(t, userId, "Song A")
		t2 := newTrackFeat(t, userId, "Song B") // no featured
		repo.Seed(t1)
		repo.Seed(t2)

		resolver := fakeResolver{byTitle: map[string][]domain.FeaturedArtist{
			"Song A": {{Name: "Guest", MBID: "m1", Role: domain.RoleFeatured}},
		}}
		svc := NewBackfillFeaturedService(repo, resolver)

		res, err := svc.Execute(ctx, userId)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Scanned != 2 || res.Updated != 1 {
			t.Fatalf("result = %+v, want scanned 2 updated 1", res)
		}
		if len(t1.FeaturedArtists) != 1 || t1.FeaturedArtists[0].Name != "Guest" {
			t.Errorf("t1 featured = %+v", t1.FeaturedArtists)
		}

		// Re-run: same result, still 1 featured on t1 (idempotent replace).
		res2, _ := svc.Execute(ctx, userId)
		if res2.Updated != 1 || len(t1.FeaturedArtists) != 1 {
			t.Errorf("re-run not idempotent: %+v / %+v", res2, t1.FeaturedArtists)
		}
	})

	t.Run("per-track resolver error is skipped, not fatal", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		repo.Seed(newTrackFeat(t, userId, "X"))
		svc := NewBackfillFeaturedService(repo, fakeResolver{err: errors.New("provider down")})
		res, err := svc.Execute(ctx, userId)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if res.Scanned != 1 || res.Updated != 0 {
			t.Fatalf("result = %+v", res)
		}
	})
}
