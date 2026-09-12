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
		t1 := newTrackFeat(t, userId, "Track A")
		t2 := newTrackFeat(t, userId, "Track B")
		repo.Seed(t1)
		repo.Seed(t2)

		resolver := fakeResolver{byTitle: map[string][]domain.FeaturedArtist{
			"Track A": {{Name: "Guest", MBID: "m1", Role: domain.RoleFeatured}},
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

	t.Run("resolver error is counted as failed", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		repo.Seed(newTrackFeat(t, userId, "X"))
		svc := NewBackfillFeaturedService(repo, fakeResolver{err: errors.New("provider down")})
		res, err := svc.Execute(ctx, userId)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if res.Failed != 1 {
			t.Fatalf("result = %+v, want failed 1", res)
		}
	})

	t.Run("repo error preserves partial progress", func(t *testing.T) {
		t1 := newTrackFeat(t, userId, "Track 1")
		t2 := newTrackFeat(t, userId, "Track 2")
		t3 := newTrackFeat(t, userId, "Track 3")
		repo := &orderedTrackRepo{
			TrackRepo:      catalogtest.NewTrackRepo(),
			order:          []*domain.Track{t1, t2, t3},
			failReplaceID:  t2.ID,
			failReplaceErr: errors.New("db down"),
		}
		repo.Seed(t1)
		repo.Seed(t2)
		repo.Seed(t3)

		resolver := fakeResolver{byTitle: map[string][]domain.FeaturedArtist{
			"Track 1": {{Name: "Guest 1", MBID: "m1", Role: domain.RoleFeatured}},
			"Track 2": {{Name: "Guest 2", MBID: "m2", Role: domain.RoleFeatured}},
			"Track 3": {{Name: "Guest 3", MBID: "m3", Role: domain.RoleFeatured}},
		}}
		svc := NewBackfillFeaturedService(repo, resolver)

		res, err := svc.Execute(ctx, userId)
		if err == nil {
			t.Fatalf("expected error from repo, got nil")
		}
		if res == nil {
			t.Fatalf("expected partial result alongside error, got nil")
		}
		if res.Updated != 1 {
			t.Fatalf("result = %+v, want updated 1 (first item persisted before the failure)", res)
		}
		if res.Failed != 1 {
			t.Fatalf("result = %+v, want failed 1", res)
		}
	})
}

type orderedTrackRepo struct {
	*catalogtest.TrackRepo
	order          []*domain.Track
	failReplaceID  domain.TrackId
	failReplaceErr error
}

func (r *orderedTrackRepo) ListForUser(_ context.Context, _ shared.UserId, limit, offset int) ([]*domain.Track, int, error) {
	total := len(r.order)
	if offset >= total {
		return nil, total, nil
	}
	end := offset + limit
	if end > total {
		end = total
	}
	return r.order[offset:end], total, nil
}

func (r *orderedTrackRepo) ReplaceFeaturedArtists(ctx context.Context, id domain.TrackId, userId shared.UserId, feats []domain.FeaturedArtist) error {
	if r.failReplaceErr != nil && id == r.failReplaceID {
		return r.failReplaceErr
	}
	return r.TrackRepo.ReplaceFeaturedArtists(ctx, id, userId, feats)
}
