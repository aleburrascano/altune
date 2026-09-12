package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"testing"

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

	t.Run("per-track persistence error is isolated, not fatal", func(t *testing.T) {
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
		if err != nil {
			t.Fatalf("a single persistence failure must not abort the job, got %v", err)
		}
		if res.Scanned != 3 {
			t.Fatalf("result = %+v, want scanned 3 (all tracks visited)", res)
		}
		if res.Updated != 2 {
			t.Fatalf("result = %+v, want updated 2 (t1 and t3 persisted around t2's failure)", res)
		}
		if res.Failed != 1 {
			t.Fatalf("result = %+v, want failed 1 (only t2)", res)
		}
		// t3 follows the failing t2 in the loop: it must still be persisted,
		// proving the failure is isolated like the resolver-failure path.
		if len(t3.FeaturedArtists) != 1 || t3.FeaturedArtists[0].Name != "Guest 3" {
			t.Errorf("t3 featured = %+v, want it persisted after t2's failure", t3.FeaturedArtists)
		}
	})

	t.Run("oversized library is bounded by the page cap", func(t *testing.T) {
		page := make([]*domain.Track, backfillPageSize)
		for i := range page {
			page[i] = newTrackFeat(t, userId, "Untagged")
		}
		// Always returns a full page and never signals exhaustion, so only the
		// page cap can stop the loop (an unbounded library would otherwise spin).
		repo := &unboundedTrackRepo{TrackRepo: catalogtest.NewTrackRepo(), page: page}
		svc := NewBackfillFeaturedService(repo, fakeResolver{})

		res, err := svc.Execute(ctx, userId)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if want := backfillMaxPages * backfillPageSize; res.Scanned != want {
			t.Fatalf("result = %+v, want scanned %d (capped at %d pages)", res, want, backfillMaxPages)
		}
	})

	t.Run("context cancellation stops the loop between pages", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		t1 := newTrackFeat(t, userId, "Track 1")
		t2 := newTrackFeat(t, userId, "Track 2")
		repo := &orderedTrackRepo{
			TrackRepo: catalogtest.NewTrackRepo(),
			order:     []*domain.Track{t1, t2},
			onList:    func() { cancel() }, // cancel after the first page is fetched
			pageSize:  1,
		}
		repo.Seed(t1)
		repo.Seed(t2)
		svc := NewBackfillFeaturedService(repo, fakeResolver{})

		res, err := svc.Execute(cctx, userId)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
		if res == nil || res.Scanned != 1 {
			t.Fatalf("result = %+v, want scanned 1 (stopped before the second page)", res)
		}
	})
}

type unboundedTrackRepo struct {
	*catalogtest.TrackRepo
	page []*domain.Track
}

func (r *unboundedTrackRepo) ListForUser(_ context.Context, _ shared.UserId, _, _ int) ([]*domain.Track, int, error) {
	return r.page, 1 << 30, nil
}

type orderedTrackRepo struct {
	*catalogtest.TrackRepo
	order          []*domain.Track
	failReplaceID  domain.TrackId
	failReplaceErr error
	onList         func() // invoked after each page is fetched
	pageSize       int    // overrides the caller's limit to force multiple pages
}

func (r *orderedTrackRepo) ListForUser(_ context.Context, _ shared.UserId, limit, offset int) ([]*domain.Track, int, error) {
	total := len(r.order)
	if offset >= total {
		return nil, total, nil
	}
	if r.pageSize > 0 {
		limit = r.pageSize
	}
	end := offset + limit
	if end > total {
		end = total
	}
	page := r.order[offset:end]
	if r.onList != nil {
		r.onList()
	}
	return page, total, nil
}

func (r *orderedTrackRepo) ReplaceFeaturedArtists(ctx context.Context, id domain.TrackId, userId shared.UserId, feats []domain.FeaturedArtist) error {
	if r.failReplaceErr != nil && id == r.failReplaceID {
		return r.failReplaceErr
	}
	return r.TrackRepo.ReplaceFeaturedArtists(ctx, id, userId, feats)
}
