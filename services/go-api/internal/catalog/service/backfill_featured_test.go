package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"sync"
	"testing"
	"time"

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
		svc := NewBackfillFeaturedService(repo, repo, resolver)
		// Idempotency is about repeated runs, not the throttle: disable the
		// cooldown so the second run is admitted.
		svc.admission = newBackfillAdmission(0, time.Now)

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

		res2, err := svc.Execute(ctx, userId)
		if err != nil {
			t.Fatalf("second Execute: %v", err)
		}
		if res2.Updated != 1 || len(t1.FeaturedArtists) != 1 {
			t.Errorf("re-run not idempotent: %+v / %+v", res2, t1.FeaturedArtists)
		}
	})

	t.Run("per-track resolver error is skipped, not fatal", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		repo.Seed(newTrackFeat(t, userId, "X"))
		svc := NewBackfillFeaturedService(repo, repo, fakeResolver{err: errors.New("provider down")})
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
		svc := NewBackfillFeaturedService(repo, repo, fakeResolver{err: errors.New("provider down")})
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
		svc := NewBackfillFeaturedService(repo, repo, resolver)

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
		svc := NewBackfillFeaturedService(repo, repo, fakeResolver{})

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
			pageSize:  1,
		}
		repo.Seed(t1)
		repo.Seed(t2)
		// Cancel while resolving the first page's only track.
		resolver := &cancelingResolver{cancelOn: "Track 1", cancel: cancel}
		svc := NewBackfillFeaturedService(repo, repo, resolver)

		res, err := svc.Execute(cctx, userId)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
		if res == nil || res.Scanned != 1 {
			t.Fatalf("result = %+v, want scanned 1 (stopped before the second page)", res)
		}
	})

	t.Run("context cancellation stops the loop within a page", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		tracks := []*domain.Track{
			newTrackFeat(t, userId, "Track 1"),
			newTrackFeat(t, userId, "Track 2"),
			newTrackFeat(t, userId, "Track 3"),
		}
		repo := &orderedTrackRepo{TrackRepo: catalogtest.NewTrackRepo(), order: tracks}
		resolver := &cancelingResolver{cancelOn: "Track 1", cancel: cancel}
		svc := NewBackfillFeaturedService(repo, repo, resolver)

		res, err := svc.Execute(cctx, userId)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
		if res == nil || res.Scanned != 1 || resolver.calls != 1 {
			t.Fatalf("result = %+v, resolver calls = %d; want 1 each (no lookups after cancel within the page)", res, resolver.calls)
		}
	})

	t.Run("hung resolver lookup times out, counts as failed, and the loop continues", func(t *testing.T) {
		repo := &orderedTrackRepo{TrackRepo: catalogtest.NewTrackRepo(), order: []*domain.Track{
			newTrackFeat(t, userId, "Hangs"),
			newTrackFeat(t, userId, "Fine"),
		}}
		// Like the discovery resolver, swallow the ctx error and return an
		// empty result: the service must still count the item as failed.
		resolver := &hangingResolver{hangOn: "Hangs"}
		svc := NewBackfillFeaturedService(repo, repo, resolver)
		svc.itemTimeout = 20 * time.Millisecond

		start := time.Now()
		res, err := svc.Execute(ctx, userId)
		if err != nil {
			t.Fatalf("a hung lookup must not abort the job, got %v", err)
		}
		if elapsed := time.Since(start); elapsed > 2*time.Second {
			t.Fatalf("Execute took %v; the per-item timeout did not bound the hung lookup", elapsed)
		}
		if res.Scanned != 2 || res.Failed != 1 {
			t.Fatalf("result = %+v, want scanned 2 failed 1", res)
		}
		if resolver.calls != 2 {
			t.Fatalf("resolver calls = %d, want 2 (the track after the hung one was still resolved)", resolver.calls)
		}
	})

	t.Run("second run while one is in flight is rejected", func(t *testing.T) {
		release := make(chan struct{})
		entered := make(chan struct{})
		repo := &orderedTrackRepo{TrackRepo: catalogtest.NewTrackRepo(), order: []*domain.Track{newTrackFeat(t, userId, "T")}}
		svc := NewBackfillFeaturedService(repo, repo, &blockingResolver{entered: entered, release: release})

		done := make(chan error, 1)
		go func() {
			_, err := svc.Execute(ctx, userId)
			done <- err
		}()
		<-entered

		if _, err := svc.Execute(ctx, userId); !errors.Is(err, ErrBackfillInProgress) {
			t.Fatalf("concurrent run: want ErrBackfillInProgress, got %v", err)
		}
		otherUser := shared.NewUserId(uuid.New())
		if _, err := svc.Execute(ctx, otherUser); err != nil {
			t.Fatalf("another user's run must not be blocked, got %v", err)
		}
		close(release)
		if err := <-done; err != nil {
			t.Fatalf("first run: %v", err)
		}
	})

	t.Run("run within cooldown is rejected until the window passes", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		repo.Seed(newTrackFeat(t, userId, "T"))
		svc := NewBackfillFeaturedService(repo, repo, fakeResolver{})
		clock := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		svc.admission = newBackfillAdmission(backfillCooldown, func() time.Time { return clock })

		if _, err := svc.Execute(ctx, userId); err != nil {
			t.Fatalf("first run: %v", err)
		}
		clock = clock.Add(backfillCooldown - time.Second)
		res, err := svc.Execute(ctx, userId)
		if !errors.Is(err, ErrBackfillCoolingDown) || res != nil {
			t.Fatalf("run within cooldown: want ErrBackfillCoolingDown and nil result, got %+v, %v", res, err)
		}
		if ErrBackfillCoolingDown.HTTPStatus() != 429 || ErrBackfillInProgress.HTTPStatus() != 409 {
			t.Fatalf("throttle errors must map to 429/409")
		}
		clock = clock.Add(time.Second)
		if _, err := svc.Execute(ctx, userId); err != nil {
			t.Fatalf("run after cooldown: %v", err)
		}
	})

	t.Run("canceled run still starts the cooldown", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		repo.Seed(newTrackFeat(t, userId, "T"))
		svc := NewBackfillFeaturedService(repo, repo, fakeResolver{})
		cctx, cancel := context.WithCancel(ctx)
		cancel()

		if _, err := svc.Execute(cctx, userId); !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
		if _, err := svc.Execute(ctx, userId); !errors.Is(err, ErrBackfillCoolingDown) {
			t.Fatalf("aborting a run must not skip the cooldown, got %v", err)
		}
	})
}

type cancelingResolver struct {
	cancelOn string
	cancel   context.CancelFunc
	calls    int
}

func (r *cancelingResolver) Resolve(_ context.Context, _, title string) ([]domain.FeaturedArtist, error) {
	r.calls++
	if title == r.cancelOn {
		r.cancel()
	}
	return nil, nil
}

type hangingResolver struct {
	hangOn string
	calls  int
}

func (r *hangingResolver) Resolve(ctx context.Context, _, title string) ([]domain.FeaturedArtist, error) {
	r.calls++
	if title == r.hangOn {
		<-ctx.Done()
	}
	return nil, nil
}

// blockingResolver parks its first lookup until release is closed, signalling
// entered once it is parked; later lookups return immediately.
type blockingResolver struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (r *blockingResolver) Resolve(context.Context, string, string) ([]domain.FeaturedArtist, error) {
	first := false
	r.once.Do(func() { first = true })
	if first {
		close(r.entered)
		<-r.release
	}
	return nil, nil
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
	pageSize       int // overrides the caller's limit to force multiple pages
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
	return page, total, nil
}

func (r *orderedTrackRepo) ReplaceFeaturedArtists(ctx context.Context, id domain.TrackId, userId shared.UserId, feats []domain.FeaturedArtist) error {
	if r.failReplaceErr != nil && id == r.failReplaceID {
		return r.failReplaceErr
	}
	return r.TrackRepo.ReplaceFeaturedArtists(ctx, id, userId, feats)
}
