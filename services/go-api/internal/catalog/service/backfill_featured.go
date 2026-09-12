package service

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
	"context"
	"fmt"
	"log/slog"
)

type BackfillFeaturedService struct {
	trackRepo    ports.TrackRepository
	featuredRepo ports.FeaturedArtistRepository
	resolver     ports.FeaturedArtistResolver
}

func NewBackfillFeaturedService(
	trackRepo ports.TrackRepository,
	featuredRepo ports.FeaturedArtistRepository,
	resolver ports.FeaturedArtistResolver,
) *BackfillFeaturedService {
	return &BackfillFeaturedService{trackRepo: trackRepo, featuredRepo: featuredRepo, resolver: resolver}
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
)

func (s *BackfillFeaturedService) Execute(ctx context.Context, userId shared.UserId) (*BackfillFeaturedResult, error) {
	res := &BackfillFeaturedResult{}
	offset := 0
	for page := 0; page < backfillMaxPages; page++ {
		// Respect the request's deadline/cancellation between pages so a slow
		// job stops promptly instead of scanning the rest of the library.
		if err := ctx.Err(); err != nil {
			return res, fmt.Errorf("featured backfill canceled: %w", err)
		}
		tracks, total, err := s.trackRepo.ListForUser(ctx, userId, backfillPageSize, offset)
		if err != nil {
			return res, fmt.Errorf("list tracks for backfill: %w", err)
		}
		if len(tracks) == 0 {
			break
		}
		for _, t := range tracks {
			s.backfillTrack(ctx, userId, t, res)
		}
		offset += len(tracks)
		if offset >= total {
			break
		}
	}
	slog.InfoContext(ctx, "featured backfill complete",
		"user_id", userId.String(), "scanned", res.Scanned, "updated", res.Updated, "failed", res.Failed)
	return res, nil
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
	feats, err := s.resolver.Resolve(ctx, t.Artist, t.Title)
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
