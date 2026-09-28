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
)

func (s *BackfillFeaturedService) Execute(ctx context.Context, userId shared.UserId, startOffset int) (*BackfillFeaturedResult, error) {
	if startOffset < 0 {
		return nil, domain.NewValidationError("offset must not be negative")
	}
	if err := s.admission.admit(userId); err != nil {
		return nil, err
	}
	defer s.admission.release(userId)

	res := &BackfillFeaturedResult{NextOffset: startOffset}
	if err := s.run(ctx, userId, res); err != nil {
		return res, err
	}
	slog.InfoContext(ctx, "featured backfill complete",
		"user_id", userId.String(), "scanned", res.Scanned, "updated", res.Updated, "failed", res.Failed,
		"truncated", res.Truncated, "next_offset", res.NextOffset)
	return res, nil
}

func (s *BackfillFeaturedService) run(ctx context.Context, userId shared.UserId, res *BackfillFeaturedResult) error {
	for page := 0; page < backfillMaxPages; page++ {
		if err := ctx.Err(); err != nil {
			return fmt.Errorf("featured backfill canceled: %w", err)
		}
		tracks, total, err := s.trackRepo.ListForUser(ctx, userId, backfillPageSize, res.NextOffset)
		if err != nil {
			return fmt.Errorf("list tracks for backfill: %w", err)
		}
		if err := s.backfillPage(ctx, userId, tracks, res); err != nil {
			return err
		}
		res.NextOffset += len(tracks)
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
			return fmt.Errorf("featured backfill canceled: %w", err)
		}
		s.backfillTrack(ctx, userId, t, res)
	}
	return nil
}

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
