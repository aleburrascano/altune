package featured

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

type BackfillFeaturedService struct {
	trackRepo    ports.TrackLister
	featuredRepo ports.FeaturedArtistRepository
	resolver     ports.FeaturedArtistResolver
	admission    *backfillAdmission
	itemTimeout  time.Duration
	timeBudget   time.Duration
}

type BackfillOption func(*BackfillFeaturedService)

func WithBackfillTimeBudget(budget time.Duration) BackfillOption {
	return func(s *BackfillFeaturedService) { s.timeBudget = budget }
}

func NewBackfillFeaturedService(
	trackRepo ports.TrackLister,
	featuredRepo ports.FeaturedArtistRepository,
	resolver ports.FeaturedArtistResolver,
	opts ...BackfillOption,
) *BackfillFeaturedService {
	s := &BackfillFeaturedService{
		trackRepo:    trackRepo,
		featuredRepo: featuredRepo,
		resolver:     resolver,
		admission:    newBackfillAdmission(backfillCooldown, time.Now),
		itemTimeout:  backfillItemTimeout,
		timeBudget:   backfillTimeBudget,
	}
	for _, opt := range opts {
		opt(s)
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
	backfillPageSize    = 200
	backfillMaxPages    = 50
	backfillCooldown    = 5 * time.Minute
	backfillItemTimeout = 10 * time.Second
	backfillTimeBudget  = 45 * time.Second
)

var errBackfillBudgetSpent = errors.New("featured backfill time budget spent")

func isBudgetSpent(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), errBackfillBudgetSpent)
}

func (s *BackfillFeaturedService) Execute(ctx context.Context, userId shared.UserId, startOffset int) (*BackfillFeaturedResult, error) {
	if startOffset < 0 {
		return nil, domain.NewValidationError("offset must not be negative")
	}
	if err := s.admission.admit(userId); err != nil {
		return nil, err
	}
	res := &BackfillFeaturedResult{NextOffset: startOffset}
	budgetCtx, cancel := context.WithTimeoutCause(ctx, s.timeBudget, errBackfillBudgetSpent)
	defer cancel()
	err := s.run(budgetCtx, userId, res)
	stoppedOnBudget := res.Truncated && isBudgetSpent(budgetCtx)
	s.admission.release(userId, !stoppedOnBudget)
	if err != nil {
		return res, err
	}
	slog.InfoContext(ctx, "featured backfill complete",
		"user_id", userId.String(), "scanned", res.Scanned, "updated", res.Updated, "failed", res.Failed,
		"truncated", res.Truncated, "next_offset", res.NextOffset)
	return res, nil
}

func (s *BackfillFeaturedService) run(ctx context.Context, userId shared.UserId, res *BackfillFeaturedResult) error {
	err := s.runPages(ctx, userId, res)
	if errors.Is(err, errBackfillBudgetSpent) {
		res.Truncated = true
		return nil
	}
	return err
}

func (s *BackfillFeaturedService) runPages(ctx context.Context, userId shared.UserId, res *BackfillFeaturedResult) error {
	for page := 0; page < backfillMaxPages; page++ {
		if err := ctx.Err(); err != nil {
			return stopReason(ctx, err)
		}
		tracks, total, err := s.trackRepo.ListForUser(ctx, userId, backfillPageSize, res.NextOffset)
		if err != nil {
			if isBudgetSpent(ctx) {
				return errBackfillBudgetSpent
			}
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

func (s *BackfillFeaturedService) backfillPage(
	ctx context.Context,
	userId shared.UserId,
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

func (s *BackfillFeaturedService) backfillTrack(
	ctx context.Context,
	userId shared.UserId,
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
			"track_id", t.ID.String(), "error", err)
		return nil
	}
	if len(feats) == 0 {
		return nil
	}
	if err := s.featuredRepo.ReplaceFeaturedArtists(context.WithoutCancel(ctx), t.ID, userId, feats); err != nil {
		res.Failed++
		slog.WarnContext(ctx, "featured backfill persist failed",
			"track_id", t.ID.String(), "error", err)
		return nil
	}
	res.Updated++
	return nil
}

func (s *BackfillFeaturedService) resolve(ctx context.Context, t *domain.Track) ([]domain.FeaturedArtist, error) {
	itemCtx, cancel := context.WithTimeout(ctx, s.itemTimeout)
	defer cancel()
	feats, err := s.resolver.Resolve(itemCtx, t.Artist, t.Title)
	if err != nil {
		return nil, err
	}
	if ctxErr := itemCtx.Err(); ctxErr != nil {
		return nil, fmt.Errorf("resolve featured artists: %w", ctxErr)
	}
	return feats, nil
}

var (
	ErrBackfillInProgress  = &domain.CodedError{Msg: "featured artist backfill already in progress", Status: 409, Code: "catalog.backfill_in_progress"}
	ErrBackfillCoolingDown = &domain.CodedError{Msg: "featured artist backfill ran recently; try again later", Status: 429, Code: "catalog.backfill_cooling_down"}
)

type backfillAdmission struct {
	cooldown time.Duration
	now      func() time.Time

	mu           sync.Mutex
	running      map[shared.UserId]struct{}
	lastFinished map[shared.UserId]time.Time
}

func newBackfillAdmission(cooldown time.Duration, now func() time.Time) *backfillAdmission {
	return &backfillAdmission{
		cooldown:     cooldown,
		now:          now,
		running:      make(map[shared.UserId]struct{}),
		lastFinished: make(map[shared.UserId]time.Time),
	}
}

func (a *backfillAdmission) admit(userId shared.UserId) error {
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

func (a *backfillAdmission) release(userId shared.UserId, startCooldown bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.running, userId)
	if startCooldown {
		a.lastFinished[userId] = a.now()
	}
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
	userId shared.UserId,
	fa domain.FeaturedArtist,
) ([]*domain.Track, error) {
	tracks, err := s.featuredRepo.ListTracksFeaturing(ctx, userId, fa)
	if err != nil {
		return nil, domain.WrapRepoError(ctx, "list featuring", err)
	}
	return tracks, nil
}
