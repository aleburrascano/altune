package service

import (
	"altune/go-api/internal/catalog/catalogtest"
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"errors"
	"fmt"
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

		res, err := svc.Execute(ctx, userId, 0)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if res.Scanned != 2 || res.Updated != 1 {
			t.Fatalf("result = %+v, want scanned 2 updated 1", res)
		}
		if len(t1.FeaturedArtists) != 1 || t1.FeaturedArtists[0].Name != "Guest" {
			t.Errorf("t1 featured = %+v", t1.FeaturedArtists)
		}

		res2, err := svc.Execute(ctx, userId, 0)
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
		res, err := svc.Execute(ctx, userId, 0)
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
		res, err := svc.Execute(ctx, userId, 0)
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

		res, err := svc.Execute(ctx, userId, 0)
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

		res, err := svc.Execute(ctx, userId, 0)
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

		res, err := svc.Execute(cctx, userId, 0)
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

		res, err := svc.Execute(cctx, userId, 0)
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
		res, err := svc.Execute(ctx, userId, 0)
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
			_, err := svc.Execute(ctx, userId, 0)
			done <- err
		}()
		<-entered

		if _, err := svc.Execute(ctx, userId, 0); !errors.Is(err, ErrBackfillInProgress) {
			t.Fatalf("concurrent run: want ErrBackfillInProgress, got %v", err)
		}
		otherUser := shared.NewUserId(uuid.New())
		if _, err := svc.Execute(ctx, otherUser, 0); err != nil {
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

		if _, err := svc.Execute(ctx, userId, 0); err != nil {
			t.Fatalf("first run: %v", err)
		}
		clock = clock.Add(backfillCooldown - time.Second)
		res, err := svc.Execute(ctx, userId, 0)
		if !errors.Is(err, ErrBackfillCoolingDown) || res != nil {
			t.Fatalf("run within cooldown: want ErrBackfillCoolingDown and nil result, got %+v, %v", res, err)
		}
		if ErrBackfillCoolingDown.HTTPStatus() != 429 || ErrBackfillInProgress.HTTPStatus() != 409 {
			t.Fatalf("throttle errors must map to 429/409")
		}
		clock = clock.Add(time.Second)
		if _, err := svc.Execute(ctx, userId, 0); err != nil {
			t.Fatalf("run after cooldown: %v", err)
		}
	})

	t.Run("a negative offset is refused without spending the cooldown", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		repo.Seed(newTrackFeat(t, userId, "T"))
		svc := NewBackfillFeaturedService(repo, repo, fakeResolver{})

		res, err := svc.Execute(ctx, userId, -1)
		if res != nil {
			t.Fatalf("result = %+v, want nil for a rejected offset", res)
		}
		if sharedtest.AssertValidationError(t, err).HTTPStatus() != 400 {
			t.Errorf("error = %v, want a 400 validation error", err)
		}
		if _, err := svc.Execute(ctx, userId, 0); err != nil {
			t.Fatalf("a run refused before it started must leave the cooldown unspent, got %v", err)
		}
	})

	t.Run("canceled run still starts the cooldown", func(t *testing.T) {
		repo := catalogtest.NewTrackRepo()
		repo.Seed(newTrackFeat(t, userId, "T"))
		svc := NewBackfillFeaturedService(repo, repo, fakeResolver{})
		cctx, cancel := context.WithCancel(ctx)
		cancel()

		if _, err := svc.Execute(cctx, userId, 0); !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
		if _, err := svc.Execute(ctx, userId, 0); !errors.Is(err, ErrBackfillCoolingDown) {
			t.Fatalf("aborting a run must not skip the cooldown, got %v", err)
		}
	})
}

// TestBackfill_LibraryOverCap_ReportsTruncated is the bug's acceptance test. A
// library past the page cap used to report a capped run as a complete one, and
// every run restarted at track 1, so the tail beyond the cap was unreachable.
func TestBackfill_LibraryOverCap_ReportsTruncated(t *testing.T) {
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	const capTracks = backfillMaxPages * backfillPageSize
	const pastTheCap = "Track 10001"
	const tail = 5

	repo := newNumberedTrackRepo(t, userId, capTracks+tail)
	resolver := fakeResolver{byTitle: map[string][]domain.FeaturedArtist{
		pastTheCap: {{Name: "Guest", MBID: "m1", Role: domain.RoleFeatured}},
	}}
	svc := NewBackfillFeaturedService(repo, repo, resolver)
	// Resuming is the behaviour under test, not the throttle: disable the
	// cooldown so the follow-up run is admitted.
	svc.admission = newBackfillAdmission(0, time.Now)

	first, err := svc.Execute(ctx, userId, 0)
	if err != nil {
		t.Fatalf("first run: %v", err)
	}
	if !first.Truncated {
		t.Fatalf("first run = %+v, want truncated: the library is %d tracks over the cap", first, tail)
	}
	if first.Scanned != capTracks || first.NextOffset != capTracks {
		t.Fatalf("first run = %+v, want scanned and next offset %d", first, capTracks)
	}

	second, err := svc.Execute(ctx, userId, first.NextOffset)
	if err != nil {
		t.Fatalf("follow-up run: %v", err)
	}
	if second.Truncated {
		t.Errorf("follow-up run = %+v, want truncated false: it reached the end of the library", second)
	}
	if second.Scanned != tail {
		t.Errorf("follow-up run = %+v, want scanned %d (the tail past the cap)", second, tail)
	}
	if feats := repo.featuredArtistsOf(pastTheCap); len(feats) != 1 || feats[0].Name != "Guest" {
		t.Errorf("%s featured = %+v, want the follow-up run to have resolved it", pastTheCap, feats)
	}
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

// numberedTrackRepo serves a library of total tracks titled "Track 1" upward in
// a stable order, building each page on demand: the cap spans over 10k tracks,
// too many to write as a fixture, and catalogtest.TrackRepo pages a map, whose
// order an offset cannot index.
type numberedTrackRepo struct {
	*catalogtest.TrackRepo
	t       *testing.T
	userId  shared.UserId
	total   int
	byTitle map[string]*domain.Track
}

func newNumberedTrackRepo(t *testing.T, userId shared.UserId, total int) *numberedTrackRepo {
	return &numberedTrackRepo{
		TrackRepo: catalogtest.NewTrackRepo(),
		t:         t,
		userId:    userId,
		total:     total,
		byTitle:   make(map[string]*domain.Track, total),
	}
}

func (r *numberedTrackRepo) ListForUser(_ context.Context, _ shared.UserId, limit, offset int) ([]*domain.Track, int, error) {
	end := min(offset+limit, r.total)
	if offset >= end {
		return nil, r.total, nil
	}
	page := make([]*domain.Track, 0, end-offset)
	for i := offset; i < end; i++ {
		page = append(page, r.track(fmt.Sprintf("Track %d", i+1)))
	}
	return page, r.total, nil
}

// track keeps one object per title, so a track listed by two runs is the same
// one the test asserts against afterwards.
func (r *numberedTrackRepo) track(title string) *domain.Track {
	if existing, ok := r.byTitle[title]; ok {
		return existing
	}
	track := newTrackFeat(r.t, r.userId, title)
	r.byTitle[title] = track
	r.Seed(track)
	return track
}

func (r *numberedTrackRepo) featuredArtistsOf(title string) []domain.FeaturedArtist {
	track, ok := r.byTitle[title]
	if !ok {
		return nil
	}
	return track.FeaturedArtists
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
