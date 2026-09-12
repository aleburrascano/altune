package service

import (
	"context"
	"fmt"
	"log/slog"

	"altune/go-api/internal/catalog/ports"
	"altune/go-api/internal/shared"
)

type BackfillFeaturedService struct {
	trackRepo ports.TrackRepository
	resolver  ports.FeaturedArtistResolver
}

func NewBackfillFeaturedService(
	trackRepo ports.TrackRepository,
	resolver ports.FeaturedArtistResolver,
) *BackfillFeaturedService {
	return &BackfillFeaturedService{trackRepo: trackRepo, resolver: resolver}
}

type BackfillFeaturedResult struct {
	Scanned int `json:"scanned"`
	Updated int `json:"updated"`
	Failed  int `json:"failed"`
}

const backfillPageSize = 200

func (s *BackfillFeaturedService) Execute(ctx context.Context, userId shared.UserId) (*BackfillFeaturedResult, error) {
	res := &BackfillFeaturedResult{}
	offset := 0
	for {
		tracks, total, err := s.trackRepo.ListForUser(ctx, userId, backfillPageSize, offset)
		if err != nil {
			return nil, fmt.Errorf("list tracks for backfill: %w", err)
		}
		if len(tracks) == 0 {
			break
		}
		for _, t := range tracks {
			res.Scanned++
			feats, err := s.resolver.Resolve(ctx, t.Artist, t.Title)
			if err != nil {
				res.Failed++
				slog.WarnContext(ctx, "featured backfill resolve failed",
					"track_id", t.ID.String(), "error", err)
				continue
			}
			if len(feats) == 0 {
				continue
			}
			if err := s.trackRepo.ReplaceFeaturedArtists(ctx, t.ID, userId, feats); err != nil {
				res.Failed++
				return res, fmt.Errorf("replace featured for %s: %w", t.ID.String(), err)
			}
			res.Updated++
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
