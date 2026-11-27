package featured

import (
	"altune/go-api/internal/library/internal/core/ports"
	"altune/go-api/internal/library/internal/domain"
	apierror "altune/go-api/internal/shared/apierror"
	"altune/go-api/internal/shared/background"
	"altune/go-api/internal/shared/clock"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/logging"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type FillFeaturedArtistsService struct {
	trackRepo    ports.TrackLister
	featuredRepo ports.FeaturedArtistRepository
	resolver     ports.FeaturedArtistResolver
	admission    *backfillAdmission
	itemTimeout  time.Duration
	timeBudget   time.Duration
}

type BackfillOption func(*FillFeaturedArtistsService)

func WithBackfillTimeBudget(budget time.Duration) BackfillOption {
	return func(s *FillFeaturedArtistsService) {
		if budget > 0 {
			s.timeBudget = budget
		}
	}
}

func WithBackfillItemTimeout(timeout time.Duration) BackfillOption {
	return func(s *FillFeaturedArtistsService) {
		if timeout > 0 {
			s.itemTimeout = timeout
		}
	}
}

var ErrClockRequired = errors.New("featured: the backfill service requires a clock")

func NewBackfillFeaturedService(
	trackRepo ports.TrackLister,
	featuredRepo ports.FeaturedArtistRepository,
	resolver ports.FeaturedArtistResolver,
	now clock.Func,
	opts ...BackfillOption,
) (*FillFeaturedArtistsService, error) {
	if now == nil {
		return nil, ErrClockRequired
	}
	s := &FillFeaturedArtistsService{
		trackRepo:    trackRepo,
		featuredRepo: featuredRepo,
		resolver:     resolver,
		admission:    newBackfillAdmission(backfillCooldown, now),
		itemTimeout:  backfillItemTimeout,
		timeBudget:   backfillTimeBudget,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s, nil
}

func MustNewBackfillFeaturedService(
	trackRepo ports.TrackLister,
	featuredRepo ports.FeaturedArtistRepository,
	resolver ports.FeaturedArtistResolver,
	now clock.Func,
	opts ...BackfillOption,
) *FillFeaturedArtistsService {
	s, err := NewBackfillFeaturedService(trackRepo, featuredRepo, resolver, now, opts...)
	if err != nil {
		panic(err)
	}
	return s
}

type BackfillFeaturedResult struct {
	Scanned    int  `json:"scanned"`
	Updated    int  `json:"updated"`
	Failed     int  `json:"failed"`
	Truncated  bool `json:"truncated"`
	NextOffset int  `json:"next_offset"`
}

const (
	backfillPageSize     = 200
	backfillMaxPages     = 50
	backfillCooldown     = 5 * time.Minute
	backfillItemTimeout  = 10 * time.Second
	backfillWriteTimeout = 5 * time.Second
	backfillTimeBudget   = 45 * time.Second
)

var errBackfillBudgetSpent = errors.New("featured backfill time budget spent")

func isBudgetSpent(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), errBackfillBudgetSpent)
}

func (s *FillFeaturedArtistsService) Execute(ctx context.Context, userId ids.UserId, startOffset int) (*BackfillFeaturedResult, error) {
	if startOffset < 0 {
		return nil, domain.NewValidationError("offset must not be negative")
	}
	if err := s.admission.admit(userId); err != nil {
		return nil, err
	}
	res := &BackfillFeaturedResult{NextOffset: startOffset}
	budgetCtx, cancel := context.WithTimeoutCause(ctx, s.timeBudget, errBackfillBudgetSpent)
	defer cancel()
	stoppedOnBudget := false
	defer func() {
		if stoppedOnBudget {
			s.admission.release(userId)
		} else {
			s.admission.releaseWithCooldown(userId)
		}
	}()
	err := s.run(budgetCtx, userId, res)
	stoppedOnBudget = res.Truncated && isBudgetSpent(budgetCtx)
	if err != nil {
		return res, err
	}
	slog.InfoContext(ctx, "featured backfill complete",
		"scanned", res.Scanned, "updated", res.Updated, "failed", res.Failed,
		"truncated", res.Truncated, "next_offset", res.NextOffset)
	return res, nil
}

func (s *FillFeaturedArtistsService) run(ctx context.Context, userId ids.UserId, res *BackfillFeaturedResult) error {
	err := s.runPages(ctx, userId, res)
	if errors.Is(err, errBackfillBudgetSpent) {
		res.Truncated = true
		return nil
	}
	return err
}

func (s *FillFeaturedArtistsService) runPages(ctx context.Context, userId ids.UserId, res *BackfillFeaturedResult) error {
	for page := 0; page < backfillMaxPages; page++ {
		if err := ctx.Err(); err != nil {
			return stopReason(ctx, err)
		}
		tracks, total, err := s.trackRepo.ListForUser(ctx, userId, backfillPageSize, res.NextOffset)
		if err != nil && isBudgetSpent(ctx) {
			return errBackfillBudgetSpent
		}
		if err != nil {
			return fmt.Errorf("list tracks for backfill: %w", err)
		}
		if err := s.backfillPage(ctx, userId, tracks, res); err != nil {
			return err
		}
		if len(tracks) == 0 || res.NextOffset >= total {
			return nil
		}
	}
	res.Truncated = true
	return nil
}

func (s *FillFeaturedArtistsService) backfillPage(
	ctx context.Context,
	userId ids.UserId,
	tracks []*domain.Track,
	res *BackfillFeaturedResult,
) error {
	for _, t := range tracks {
		if err := ctx.Err(); err != nil {
			return stopReason(ctx, err)
		}
		if err := s.backfillTrack(ctx, userId, t, res); err != nil {
			return stopReason(ctx, err)
		}
		res.NextOffset++
	}
	return nil
}

func stopReason(ctx context.Context, err error) error {
	if isBudgetSpent(ctx) {
		return errBackfillBudgetSpent
	}
	return fmt.Errorf("featured backfill canceled: %w", err)
}

func (s *FillFeaturedArtistsService) backfillTrack(
	ctx context.Context,
	userId ids.UserId,
	t *domain.Track,
	res *BackfillFeaturedResult,
) error {
	feats, err := s.resolve(ctx, t)
	if isBudgetSpent(ctx) {
		return errBackfillBudgetSpent
	}
	res.Scanned++
	if err != nil {
		res.Failed++
		slog.WarnContext(ctx, "featured backfill resolve failed",
			logging.KeyTrackID, t.ID().String(), logging.KeyError, err)
		return nil
	}
	if len(feats) == 0 {
		return nil
	}
	if err := s.persist(ctx, t.ID(), userId, feats); err != nil {
		res.Failed++
		slog.WarnContext(ctx, "featured backfill persist failed",
			logging.KeyTrackID, t.ID().String(), logging.KeyError, err)
		return nil
	}
	res.Updated++
	return nil
}

func (s *FillFeaturedArtistsService) persist(ctx context.Context, id domain.TrackId, userId ids.UserId, feats []domain.FeaturedArtist) error {
	writeCtx, cancel := background.Detach(ctx, backfillWriteTimeout)
	defer cancel()
	return s.featuredRepo.ReplaceFeaturedArtists(writeCtx, id, userId, feats)
}

func (s *FillFeaturedArtistsService) resolve(ctx context.Context, t *domain.Track) ([]domain.FeaturedArtist, error) {
	itemCtx, cancel := context.WithTimeout(ctx, s.itemTimeout)
	defer cancel()
	feats, err := s.resolver.Resolve(itemCtx, t.Artist(), t.Title())
	if err != nil {
		return nil, err
	}
	if ctxErr := itemCtx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("resolve featured artists: %w", ctxErr)
	}
	return feats, nil
}

var (
	ErrBackfillInProgress  = apierror.NewError(apierror.CodeCatalogBackfillInProgress, "featured artist backfill already in progress")
	ErrBackfillCoolingDown = apierror.NewError(apierror.CodeCatalogBackfillCoolingDown, "featured artist backfill ran recently; try again later")
)

type backfillAdmission struct {
	cooldown time.Duration
	now      clock.Func

	mu           sync.Mutex
	running      map[ids.UserId]struct{}
	lastFinished map[ids.UserId]time.Time
}

func newBackfillAdmission(cooldown time.Duration, now clock.Func) *backfillAdmission {
	return &backfillAdmission{
		cooldown:     cooldown,
		now:          now,
		running:      make(map[ids.UserId]struct{}),
		lastFinished: make(map[ids.UserId]time.Time),
	}
}

func (a *backfillAdmission) admit(userId ids.UserId) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneExpired()
	if _, busy := a.running[userId]; busy {
		return ErrBackfillInProgress
	}
	if _, recent := a.lastFinished[userId]; recent {
		return ErrBackfillCoolingDown
	}
	a.running[userId] = struct{}{}
	return nil
}

func (a *backfillAdmission) release(userId ids.UserId) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.running, userId)
}

func (a *backfillAdmission) releaseWithCooldown(userId ids.UserId) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.running, userId)
	a.lastFinished[userId] = a.now()
}

func (a *backfillAdmission) pruneExpired() {
	now := a.now()
	for id, finished := range a.lastFinished {
		if now.Sub(finished) >= a.cooldown {
			delete(a.lastFinished, id)
		}
	}
}

type ListFeaturingService struct {
	featuredRepo ports.FeaturedArtistRepository
}

func NewListFeaturingService(featuredRepo ports.FeaturedArtistRepository) *ListFeaturingService {
	return &ListFeaturingService{featuredRepo: featuredRepo}
}

func (s *ListFeaturingService) Execute(
	ctx context.Context,
	userId ids.UserId,
	fa domain.FeaturedArtist,
) ([]*domain.Track, error) {
	tracks, err := s.featuredRepo.ListTracksFeaturing(ctx, userId, fa)
	if err != nil {
		return nil, domain.WrapRepoError(ctx, "list featuring", err)
	}
	return tracks, nil
}
