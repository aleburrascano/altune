package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
	"log/slog"
	"time"
)

type BackfillFeaturedService struct {
	trackRepo    ports.TrackLister
	featuredRepo ports.FeaturedArtistRepository
	resolver     ports.FeaturedArtistResolver
	admission    *backfillAdmission
	itemTimeout  time.Duration
}

func NewBackfillFeaturedService(
	trackRepo ports.TrackLister,
	featuredRepo ports.FeaturedArtistRepository,
	resolver ports.FeaturedArtistResolver,
) *BackfillFeaturedService {
	return &BackfillFeaturedService{
		trackRepo:    trackRepo,
		featuredRepo: featuredRepo,
		resolver:     resolver,
		admission:    newBackfillAdmission(backfillCooldown, time.Now),
		itemTimeout:  backfillItemTimeout,
	}
}

type BackfillFeaturedResult struct {
	Scanned int `json:"scanned"`
	Updated int `json:"updated"`
	Failed  int `json:"failed"`
}

const (
	backfillPageSize = 200
	// backfillMaxPages bounds the per-request work: the service runs inside one
	// synchronous HTTP request, so a huge library must not produce an unbounded
	// request. At backfillPageSize that caps a single invocation at 10k tracks;
	// anything beyond is left for a subsequent run.
	backfillMaxPages = 50
	// backfillCooldown is the minimum gap between the end of one run and the
	// start of the next for the same user, so back-to-back calls cannot sustain
	// a continuous stream of up to 10k external lookups per request.
	backfillCooldown = 5 * time.Minute
	// backfillItemTimeout bounds a single resolver lookup so one hung provider
	// call is counted as failed and skipped instead of stalling the request.
	backfillItemTimeout = 10 * time.Second
)

// Execute backfills featured artists across the user's library. It returns
// ErrBackfillInProgress if the user already has a run in flight and
// ErrBackfillCoolingDown if their previous run ended within backfillCooldown.
func (s *BackfillFeaturedService) Execute(ctx context.Context, userId shared.UserId) (*BackfillFeaturedResult, error) {
	if err := s.admission.admit(userId); err != nil {
		return nil, err
	}
	defer s.admission.release(userId)

	res := &BackfillFeaturedResult{}
	if err := s.run(ctx, userId, res); err != nil {
		return res, err
	}
	slog.InfoContext(ctx, "featured backfill complete",
		"user_id", userId.String(), "scanned", res.Scanned, "updated", res.Updated, "failed", res.Failed)
	return res, nil
}

func (s *BackfillFeaturedService) run(ctx context.Context, userId shared.UserId, res *BackfillFeaturedResult) error {
	offset := 0
	for page := 0; page < backfillMaxPages; page++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("featured backfill canceled: %w", err)
		}
		tracks, total, err := s.trackRepo.ListForUser(ctx, userId, backfillPageSize, offset)
		if err != nil {
			return fmt.Errorf("list tracks for backfill: %w", err)
		}
		if err := s.backfillPage(ctx, userId, tracks, res); err != nil {
			return err
		}
		offset += len(tracks)
		if len(tracks) == 0 || offset >= total {
			return nil
		}
	}
	return nil
}

// backfillPage processes one page, rechecking cancellation before every track
// rather than once per page: a page is up to backfillPageSize external lookups,
// far too coarse to stop a slow job promptly.
func (s *BackfillFeaturedService) backfillPage(
	ctx context.Context,
	userId shared.UserId,
	tracks []*domain.Track,
	res *BackfillFeaturedResult,
) error {
	for _, t := range tracks {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("featured backfill canceled: %w", err)
		}
		s.backfillTrack(ctx, userId, t, res)
	}
	return nil
}

// backfillTrack resolves and persists featured artists for a single track.
// A per-item failure of either the resolver or the persistence step is
// contained the same way (counted as failed, logged, skipped) so one bad track
// never discards the remaining unprocessed tracks.
func (s *BackfillFeaturedService) backfillTrack(
	ctx context.Context,
	userId shared.UserId,
	t *domain.Track,
	res *BackfillFeaturedResult,
) {
	res.Scanned++
	feats, err := s.resolve(ctx, t)
	if err != nil {
		res.Failed++
		slog.WarnContext(ctx, "featured backfill resolve failed",
			"track_id", t.ID.String(), "error", err)
		return
	}
	if len(feats) == 0 {
		return
	}
	if err := s.featuredRepo.ReplaceFeaturedArtists(ctx, t.ID, userId, feats); err != nil {
		res.Failed++
		slog.WarnContext(ctx, "featured backfill persist failed",
			"track_id", t.ID.String(), "error", err)
		return
	}
	res.Updated++
}

// resolve runs one resolver lookup under the per-item timeout. The discovery
// resolver swallows provider errors (a timed-out lookup comes back as an empty,
// nil-error result), so an expired item deadline is turned into an error here;
// otherwise a hung lookup would be miscounted as "no featured artists" instead
// of as a failure.
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
