package featured_test

import (
	"altune/go-api/internal/catalog"
	shared "altune/go-api/internal/shared/kernel"
	"altune/go-api/internal/shared/sharedtest"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
)

const (
	backfillPageSize    = 200
	backfillMaxPages    = 50
	backfillCooldown    = 5 * time.Minute
	backfillItemTimeout = 10 * time.Second
	backfillTimeBudget  = 45 * time.Second
)

type manualClock struct{ at time.Time }

func newManualClock() *manualClock {
	return &manualClock{at: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *manualClock) now() time.Time { return c.at }

func (c *manualClock) advance(d time.Duration) { c.at = c.at.Add(d) }

func newTrackFeat(t *testing.T, userId shared.UserId, title string) *catalog.Track {
	t.Helper()
	track, err := catalog.NewTrack(userId, title, "Artist", "Album")
	if err != nil {
		t.Fatalf("NewTrack: %v", err)
	}
	return track
}

func TestBackfillFeaturedService(t *testing.T) {
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())

	t.Run("resolves and persists, idempotent", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		t1 := newTrackFeat(t, userId, "Track A")
		t2 := newTrackFeat(t, userId, "Track B")
		repo.Seed(t1)
		repo.Seed(t2)

		resolver := catalog.InMemoryFeaturedResolver{ByTitle: map[string][]catalog.FeaturedArtist{
			"Track A": {catalog.NewFeaturedArtistIdentityOnly("Guest", "m1", 0)},
		}}
		clock := newManualClock()
		svc := catalog.NewBackfillFeaturedService(repo, repo, resolver, catalog.WithBackfillClock(clock.now))

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

		clock.advance(backfillCooldown)
		res2, err := svc.Execute(ctx, userId, 0)
		if err != nil {
			t.Fatalf("second Execute: %v", err)
		}
		if res2.Updated != 1 || len(t1.FeaturedArtists) != 1 {
			t.Errorf("re-run not idempotent: %+v / %+v", res2, t1.FeaturedArtists)
		}
	})

	t.Run("per-track resolver error is skipped, not fatal", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		repo.Seed(newTrackFeat(t, userId, "X"))
		svc := catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{Err: errors.New("provider down")})
		res, err := svc.Execute(ctx, userId, 0)
		if err != nil {
			t.Fatalf("expected nil error, got %v", err)
		}
		if res.Scanned != 1 || res.Updated != 0 {
			t.Fatalf("result = %+v", res)
		}
	})

	t.Run("resolver error is counted as failed", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		repo.Seed(newTrackFeat(t, userId, "X"))
		svc := catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{Err: errors.New("provider down")})
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
			InMemoryTrackRepo: catalog.NewInMemoryTrackRepo(),
			order:             []*catalog.Track{t1, t2, t3},
			failReplaceID:     t2.ID,
			failReplaceErr:    errors.New("db down"),
		}
		repo.Seed(t1)
		repo.Seed(t2)
		repo.Seed(t3)

		resolver := catalog.InMemoryFeaturedResolver{ByTitle: map[string][]catalog.FeaturedArtist{
			"Track 1": {catalog.NewFeaturedArtistIdentityOnly("Guest 1", "m1", 0)},
			"Track 2": {catalog.NewFeaturedArtistIdentityOnly("Guest 2", "m2", 0)},
			"Track 3": {catalog.NewFeaturedArtistIdentityOnly("Guest 3", "m3", 0)},
		}}
		svc := catalog.NewBackfillFeaturedService(repo, repo, resolver)

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
		if len(t3.FeaturedArtists) != 1 || t3.FeaturedArtists[0].Name != "Guest 3" {
			t.Errorf("t3 featured = %+v, want it persisted after t2's failure", t3.FeaturedArtists)
		}
	})

	t.Run("oversized library is bounded by the page cap", func(t *testing.T) {
		page := make([]*catalog.Track, backfillPageSize)
		for i := range page {
			page[i] = newTrackFeat(t, userId, "Untagged")
		}
		repo := &unboundedTrackRepo{InMemoryTrackRepo: catalog.NewInMemoryTrackRepo(), page: page}
		svc := catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{})

		res, err := svc.Execute(ctx, userId, 0)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if want := backfillMaxPages * backfillPageSize; res.Scanned != want {
			t.Fatalf("result = %+v, want scanned %d (capped at %d pages)", res, want, backfillMaxPages)
		}
		if len(repo.limits) != backfillMaxPages {
			t.Fatalf("listed %d pages, want %d", len(repo.limits), backfillMaxPages)
		}
		for i, limit := range repo.limits {
			if limit != backfillPageSize {
				t.Fatalf("page %d asked for %d tracks, want %d", i, limit, backfillPageSize)
			}
		}
	})

	t.Run("context cancellation stops the loop between pages", func(t *testing.T) {
		cctx, cancel := context.WithCancel(ctx)
		t1 := newTrackFeat(t, userId, "Track 1")
		t2 := newTrackFeat(t, userId, "Track 2")
		repo := &orderedTrackRepo{
			InMemoryTrackRepo: catalog.NewInMemoryTrackRepo(),
			order:             []*catalog.Track{t1, t2},
			pageSize:          1,
		}
		repo.Seed(t1)
		repo.Seed(t2)
		resolver := &cancelingResolver{cancelOn: "Track 1", cancel: cancel}
		svc := catalog.NewBackfillFeaturedService(repo, repo, resolver)

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
		tracks := []*catalog.Track{
			newTrackFeat(t, userId, "Track 1"),
			newTrackFeat(t, userId, "Track 2"),
			newTrackFeat(t, userId, "Track 3"),
		}
		repo := &orderedTrackRepo{InMemoryTrackRepo: catalog.NewInMemoryTrackRepo(), order: tracks}
		resolver := &cancelingResolver{cancelOn: "Track 1", cancel: cancel}
		svc := catalog.NewBackfillFeaturedService(repo, repo, resolver)

		res, err := svc.Execute(cctx, userId, 0)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
		if res == nil || res.Scanned != 1 || resolver.calls != 1 {
			t.Fatalf("result = %+v, resolver calls = %d; want 1 each (no lookups after cancel within the page)", res, resolver.calls)
		}
	})

	t.Run("hung resolver lookup times out, counts as failed, and the loop continues", func(t *testing.T) {
		repo := &orderedTrackRepo{InMemoryTrackRepo: catalog.NewInMemoryTrackRepo(), order: []*catalog.Track{
			newTrackFeat(t, userId, "Hangs"),
			newTrackFeat(t, userId, "Fine"),
		}}
		resolver := &hangingResolver{hangOn: "Hangs"}
		svc := catalog.NewBackfillFeaturedService(repo, repo, resolver, catalog.WithBackfillItemTimeout(20*time.Millisecond))

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
		repo := &orderedTrackRepo{InMemoryTrackRepo: catalog.NewInMemoryTrackRepo(), order: []*catalog.Track{newTrackFeat(t, userId, "T")}}
		svc := catalog.NewBackfillFeaturedService(repo, repo, &blockingResolver{entered: entered, release: release})

		done := make(chan error, 1)
		go func() {
			_, err := svc.Execute(ctx, userId, 0)
			done <- err
		}()
		<-entered

		if _, err := svc.Execute(ctx, userId, 0); !errors.Is(err, catalog.ErrBackfillInProgress) {
			t.Fatalf("concurrent run: want catalog.ErrBackfillInProgress, got %v", err)
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
		repo := catalog.NewInMemoryTrackRepo()
		repo.Seed(newTrackFeat(t, userId, "T"))
		clock := newManualClock()
		svc := catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{}, catalog.WithBackfillClock(clock.now))

		if _, err := svc.Execute(ctx, userId, 0); err != nil {
			t.Fatalf("first run: %v", err)
		}
		clock.advance(backfillCooldown - time.Second)
		res, err := svc.Execute(ctx, userId, 0)
		if !errors.Is(err, catalog.ErrBackfillCoolingDown) || res != nil {
			t.Fatalf("run within cooldown: want catalog.ErrBackfillCoolingDown and nil result, got %+v, %v", res, err)
		}
		if catalog.ErrBackfillCoolingDown.HTTPStatus() != 429 || catalog.ErrBackfillInProgress.HTTPStatus() != 409 {
			t.Fatalf("throttle errors must map to 429/409")
		}
		clock.advance(time.Second)
		if _, err := svc.Execute(ctx, userId, 0); err != nil {
			t.Fatalf("run after cooldown: %v", err)
		}
	})

	t.Run("a negative offset is refused without spending the cooldown", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		repo.Seed(newTrackFeat(t, userId, "T"))
		svc := catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{})

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
		repo := catalog.NewInMemoryTrackRepo()
		repo.Seed(newTrackFeat(t, userId, "T"))
		svc := catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{})
		cctx, cancel := context.WithCancel(ctx)
		cancel()

		if _, err := svc.Execute(cctx, userId, 0); !errors.Is(err, context.Canceled) {
			t.Fatalf("want context.Canceled, got %v", err)
		}
		if _, err := svc.Execute(ctx, userId, 0); !errors.Is(err, catalog.ErrBackfillCoolingDown) {
			t.Fatalf("aborting a run must not skip the cooldown, got %v", err)
		}
	})
}

func TestBackfill_LibraryOverCap_ReportsTruncated(t *testing.T) {
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	const capTracks = backfillMaxPages * backfillPageSize
	const pastTheCap = "Track 10001"
	const tail = 5

	repo := newNumberedTrackRepo(t, userId, capTracks+tail)
	resolver := catalog.InMemoryFeaturedResolver{ByTitle: map[string][]catalog.FeaturedArtist{
		pastTheCap: {catalog.NewFeaturedArtistIdentityOnly("Guest", "m1", 0)},
	}}
	clock := newManualClock()
	svc := catalog.NewBackfillFeaturedService(repo, repo, resolver, catalog.WithBackfillClock(clock.now))

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

	clock.advance(backfillCooldown)
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

func (r *cancelingResolver) Resolve(_ context.Context, _, title string) ([]catalog.FeaturedArtist, error) {
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

func (r *hangingResolver) Resolve(ctx context.Context, _, title string) ([]catalog.FeaturedArtist, error) {
	r.calls++
	if title == r.hangOn {
		<-ctx.Done()
	}
	return nil, nil
}

type blockingResolver struct {
	once    sync.Once
	entered chan struct{}
	release chan struct{}
}

func (r *blockingResolver) Resolve(context.Context, string, string) ([]catalog.FeaturedArtist, error) {
	first := false
	r.once.Do(func() { first = true })
	if first {
		close(r.entered)
		<-r.release
	}
	return nil, nil
}

type unboundedTrackRepo struct {
	*catalog.InMemoryTrackRepo
	page   []*catalog.Track
	limits []int
}

func (r *unboundedTrackRepo) ListForUser(_ context.Context, _ shared.UserId, limit, _ int) ([]*catalog.Track, int, error) {
	r.limits = append(r.limits, limit)
	return r.page, 1 << 30, nil
}

type numberedTrackRepo struct {
	*catalog.InMemoryTrackRepo
	t       *testing.T
	userId  shared.UserId
	total   int
	byTitle map[string]*catalog.Track
}

func newNumberedTrackRepo(t *testing.T, userId shared.UserId, total int) *numberedTrackRepo {
	return &numberedTrackRepo{
		InMemoryTrackRepo: catalog.NewInMemoryTrackRepo(),
		t:                 t,
		userId:            userId,
		total:             total,
		byTitle:           make(map[string]*catalog.Track, total),
	}
}

func (r *numberedTrackRepo) ListForUser(_ context.Context, _ shared.UserId, limit, offset int) ([]*catalog.Track, int, error) {
	end := min(offset+limit, r.total)
	if offset >= end {
		return nil, r.total, nil
	}
	page := make([]*catalog.Track, 0, end-offset)
	for i := offset; i < end; i++ {
		page = append(page, r.track(fmt.Sprintf("Track %d", i+1)))
	}
	return page, r.total, nil
}

func (r *numberedTrackRepo) track(title string) *catalog.Track {
	if existing, ok := r.byTitle[title]; ok {
		return existing
	}
	track := newTrackFeat(r.t, r.userId, title)
	r.byTitle[title] = track
	r.Seed(track)
	return track
}

func (r *numberedTrackRepo) featuredArtistsOf(title string) []catalog.FeaturedArtist {
	track, ok := r.byTitle[title]
	if !ok {
		return nil
	}
	return track.FeaturedArtists
}

type orderedTrackRepo struct {
	*catalog.InMemoryTrackRepo
	order          []*catalog.Track
	failReplaceID  catalog.TrackId
	failReplaceErr error
	pageSize       int
}

func (r *orderedTrackRepo) ListForUser(_ context.Context, _ shared.UserId, limit, offset int) ([]*catalog.Track, int, error) {
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

func (r *orderedTrackRepo) ReplaceFeaturedArtists(ctx context.Context, id catalog.TrackId, userId shared.UserId, feats []catalog.FeaturedArtist) error {
	if r.failReplaceErr != nil && id == r.failReplaceID {
		return r.failReplaceErr
	}
	return r.InMemoryTrackRepo.ReplaceFeaturedArtists(ctx, id, userId, feats)
}

type stallsAfterFirstResolver struct {
	calls int
}

func (r *stallsAfterFirstResolver) Resolve(ctx context.Context, _, _ string) ([]catalog.FeaturedArtist, error) {
	r.calls++
	if r.calls > 1 {
		<-ctx.Done()
	}
	return nil, nil
}

func TestBackfill_TimeBudgetSpent_TruncatesAndSkipsCooldown(t *testing.T) {
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	repo := newNumberedTrackRepo(t, userId, 3)
	svc := catalog.NewBackfillFeaturedService(repo, repo, &stallsAfterFirstResolver{}, catalog.WithBackfillTimeBudget(50*time.Millisecond))

	start := time.Now()
	res, err := svc.Execute(ctx, userId, 0)
	if err != nil {
		t.Fatalf("a spent time budget must not fail the run, got %v", err)
	}
	if elapsed := time.Since(start); elapsed > 2*time.Second {
		t.Fatalf("Execute took %v; the time budget did not bound the run", elapsed)
	}
	if !res.Truncated || res.NextOffset != 1 || res.Scanned != 1 {
		t.Fatalf("result = %+v, want truncated with scanned 1 and next offset 1", res)
	}

	resumed, err := svc.Execute(ctx, userId, res.NextOffset)
	if errors.Is(err, catalog.ErrBackfillCoolingDown) {
		t.Fatalf("resume after a budget-truncated run was refused: %v", err)
	}
	if err != nil || resumed == nil {
		t.Fatalf("resume: %+v, %v", resumed, err)
	}
}

type stallingListRepo struct {
	*catalog.InMemoryTrackRepo
}

func (r *stallingListRepo) ListForUser(ctx context.Context, _ shared.UserId, _, _ int) ([]*catalog.Track, int, error) {
	<-ctx.Done()
	return nil, 0, ctx.Err()
}

func TestBackfill_TimeBudgetSpentInsideListing_TruncatesAndSkipsCooldown(t *testing.T) {
	ctx := context.Background()
	userId := shared.NewUserId(uuid.New())
	repo := &stallingListRepo{InMemoryTrackRepo: catalog.NewInMemoryTrackRepo()}
	svc := catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{}, catalog.WithBackfillTimeBudget(50*time.Millisecond))

	res, err := svc.Execute(ctx, userId, 7)
	if err != nil {
		t.Fatalf("a budget spent inside the listing must not fail the run, got %v", err)
	}
	if !res.Truncated || res.NextOffset != 7 {
		t.Fatalf("result = %+v, want truncated with next offset 7", res)
	}

	if _, err := svc.Execute(ctx, userId, res.NextOffset); errors.Is(err, catalog.ErrBackfillCoolingDown) {
		t.Fatalf("resume after a budget-truncated run was refused: %v", err)
	}
}

type featuringErrLister struct {
	catalog.FeaturedArtistRepository
	err error
}

func (f featuringErrLister) ListTracksFeaturing(_ context.Context, _ shared.UserId, _ catalog.FeaturedArtist) ([]*catalog.Track, error) {
	return nil, f.err
}

func TestListFeaturingService_Execute(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	sza := catalog.NewFeaturedArtistIdentityOnly("SZA", "", 0)
	drake := catalog.NewFeaturedArtistIdentityOnly("Drake", "", 0)

	t.Run("returns only the tracks featuring the queried artist", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		withSza := catalog.SeedTrack(t, repo, userId, "Feature", "Artist", "Album")
		withSza.FeaturedArtists = []catalog.FeaturedArtist{sza}
		withDrake := catalog.SeedTrack(t, repo, userId, "Other", "Artist", "Album")
		withDrake.FeaturedArtists = []catalog.FeaturedArtist{drake}
		catalog.SeedTrack(t, repo, userId, "Plain", "Artist", "Album")
		svc := catalog.NewListFeaturingService(repo)

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
		repo := catalog.NewInMemoryTrackRepo()
		withDrake := catalog.SeedTrack(t, repo, userId, "Other", "Artist", "Album")
		withDrake.FeaturedArtists = []catalog.FeaturedArtist{drake}
		svc := catalog.NewListFeaturingService(repo)

		got, err := svc.Execute(ctx, userId, sza)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})

	t.Run("excludes a matching track owned by another user", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		other := shared.NewUserId(uuid.MustParse("bbbbbbbb-bbbb-bbbb-bbbb-bbbbbbbbbbbb"))
		theirs := catalog.SeedTrack(t, repo, other, "Feature", "Artist", "Album")
		theirs.FeaturedArtists = []catalog.FeaturedArtist{sza}
		svc := catalog.NewListFeaturingService(repo)

		got, err := svc.Execute(ctx, userId, sza)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("len = %d, want 0", len(got))
		}
	})

	t.Run("matches by identity key regardless of the queried display name", func(t *testing.T) {
		repo := catalog.NewInMemoryTrackRepo()
		stored := catalog.NewFeaturedArtistIdentityOnly("SZA", "mbid-1", 0)
		track := catalog.SeedTrack(t, repo, userId, "Feature", "Artist", "Album")
		track.FeaturedArtists = []catalog.FeaturedArtist{stored}
		svc := catalog.NewListFeaturingService(repo)

		query := catalog.NewFeaturedArtistIdentityOnly("Solana", "mbid-1", 0)
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
		svc := catalog.NewListFeaturingService(featuringErrLister{err: errRepo})

		_, err := svc.Execute(ctx, userId, sza)

		if !errors.Is(err, errRepo) {
			t.Fatalf("error = %v, want %v", err, errRepo)
		}
		if !strings.Contains(err.Error(), "list featuring") {
			t.Errorf("error = %q, want it to wrap with 'list featuring'", err.Error())
		}
	})
}

type deadlineRecordingRepo struct {
	*catalog.InMemoryTrackRepo
	track     *catalog.Track
	deadlines []time.Time
}

func (r *deadlineRecordingRepo) ListForUser(ctx context.Context, _ shared.UserId, _, offset int) ([]*catalog.Track, int, error) {
	deadline, _ := ctx.Deadline()
	r.deadlines = append(r.deadlines, deadline)
	if offset > 0 {
		return nil, 1, nil
	}
	return []*catalog.Track{r.track}, 1, nil
}

type deadlineRecordingResolver struct {
	deadlines []time.Time
}

func (r *deadlineRecordingResolver) Resolve(ctx context.Context, _, _ string) ([]catalog.FeaturedArtist, error) {
	deadline, _ := ctx.Deadline()
	r.deadlines = append(r.deadlines, deadline)
	return nil, nil
}

func assertOneDeadline(t *testing.T, what string, got []time.Time, before, after time.Time, bound time.Duration) {
	t.Helper()
	if len(got) != 1 || got[0].Before(before.Add(bound)) || got[0].After(after.Add(bound)) {
		t.Errorf("%s deadlines = %v, want one %s past the call (%v to %v)", what, got, bound, before.Add(bound), after.Add(bound))
	}
}

func TestBackfill_BoundsEachLookupAndTheWholeRun(t *testing.T) {
	cases := []struct {
		name  string
		build func(*deadlineRecordingRepo, *deadlineRecordingResolver) *catalog.BackfillFeaturedService
	}{
		{"defaults", func(repo *deadlineRecordingRepo, res *deadlineRecordingResolver) *catalog.BackfillFeaturedService {
			return catalog.NewBackfillFeaturedService(repo, repo, res)
		}},
		{"zero overrides keep the defaults", func(repo *deadlineRecordingRepo, res *deadlineRecordingResolver) *catalog.BackfillFeaturedService {
			return catalog.NewBackfillFeaturedService(repo, repo, res, catalog.WithBackfillItemTimeout(0), catalog.WithBackfillTimeBudget(0))
		}},
		{"negative overrides keep the defaults", func(repo *deadlineRecordingRepo, res *deadlineRecordingResolver) *catalog.BackfillFeaturedService {
			return catalog.NewBackfillFeaturedService(repo, repo, res, catalog.WithBackfillItemTimeout(-time.Second), catalog.WithBackfillTimeBudget(-time.Second))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			userId := catalog.FixtureUserId()
			repo := &deadlineRecordingRepo{InMemoryTrackRepo: catalog.NewInMemoryTrackRepo(), track: newTrackFeat(t, userId, "T")}
			resolver := &deadlineRecordingResolver{}
			svc := tc.build(repo, resolver)

			before := time.Now()
			if _, err := svc.Execute(context.Background(), userId, 0); err != nil {
				t.Fatalf("Execute: %v", err)
			}
			after := time.Now()

			assertOneDeadline(t, "lookup", resolver.deadlines, before, after, backfillItemTimeout)
			assertOneDeadline(t, "listing", repo.deadlines, before, after, backfillTimeBudget)
		})
	}
}

func TestBackfill_ANilClockKeepsTheWallClock(t *testing.T) {
	ctx := context.Background()
	userId := catalog.FixtureUserId()
	repo := catalog.NewInMemoryTrackRepo()
	repo.Seed(newTrackFeat(t, userId, "T"))
	svc := catalog.NewBackfillFeaturedService(repo, repo, catalog.InMemoryFeaturedResolver{}, catalog.WithBackfillClock(nil))

	if _, err := svc.Execute(ctx, userId, 0); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if _, err := svc.Execute(ctx, userId, 0); !errors.Is(err, catalog.ErrBackfillCoolingDown) {
		t.Fatalf("an immediate rerun = %v, want ErrBackfillCoolingDown on the wall clock", err)
	}
}

type canceledDuringWriteRepo struct {
	*catalog.InMemoryTrackRepo
	cancelRun context.CancelFunc
}

func (r *canceledDuringWriteRepo) ReplaceFeaturedArtists(ctx context.Context, id catalog.TrackId, userId shared.UserId, feats []catalog.FeaturedArtist) error {
	r.cancelRun()
	if err := ctx.Err(); err != nil {
		return err
	}
	return r.InMemoryTrackRepo.ReplaceFeaturedArtists(ctx, id, userId, feats)
}

func TestBackfill_ARunCanceledDuringAWriteStillLandsThatWrite(t *testing.T) {
	userId := catalog.FixtureUserId()
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := newTrackFeat(t, userId, "Track 1")
	second := newTrackFeat(t, userId, "Track 2")
	repo := &orderedTrackRepo{InMemoryTrackRepo: catalog.NewInMemoryTrackRepo(), order: []*catalog.Track{first, second}}
	repo.Seed(first)
	repo.Seed(second)
	writes := &canceledDuringWriteRepo{InMemoryTrackRepo: repo.InMemoryTrackRepo, cancelRun: cancel}
	resolver := catalog.InMemoryFeaturedResolver{ByTitle: map[string][]catalog.FeaturedArtist{
		"Track 1": {catalog.NewFeaturedArtistIdentityOnly("Guest 1", "m1", 0)},
		"Track 2": {catalog.NewFeaturedArtistIdentityOnly("Guest 2", "m2", 0)},
	}}
	svc := catalog.NewBackfillFeaturedService(repo, writes, resolver)

	res, err := svc.Execute(runCtx, userId, 0)

	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled once the run notices", err)
	}
	if res == nil || res.Updated != 1 || res.Failed != 0 || res.NextOffset != 1 {
		t.Fatalf("result = %+v, want the write in flight counted as updated and the run stopped after it", res)
	}
	if len(first.FeaturedArtists) != 1 || first.FeaturedArtists[0].Name != "Guest 1" {
		t.Fatalf("first track featured = %+v, want the write that was in flight at cancel to have landed", first.FeaturedArtists)
	}
	if len(second.FeaturedArtists) != 0 {
		t.Fatalf("second track featured = %+v, want nothing written after the cancel", second.FeaturedArtists)
	}
}
