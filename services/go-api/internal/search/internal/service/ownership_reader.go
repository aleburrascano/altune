package service

import (
	"altune/go-api/internal/search/internal/domain"
	"altune/go-api/internal/search/internal/ports"
	"altune/go-api/internal/shared/ids"
	"altune/go-api/internal/shared/music"
	"context"
	"fmt"
	"log/slog"
)

type ownershipReader struct {
	tracks ports.OwnedTrackLister
}

func NewOwnershipReader(tracks ports.OwnedTrackLister) ports.OwnershipReader {
	return ownershipReader{tracks: tracks}
}

func (r ownershipReader) OwnedByTitleArtist(ctx context.Context, userId ids.UserId) (map[string]ports.OwnedTrack, error) {
	tracks, err := r.tracks.ListOwnedTracks(ctx, userId)
	if err != nil {
		return nil, fmt.Errorf("read owned tracks: %w", err)
	}
	owned := make(map[string]ports.OwnedTrack, len(tracks))
	for _, track := range tracks {
		track.ISRC = music.CanonicalISRC(track.ISRC)
		claim(ctx, owned, domain.OwnershipISRCKey(track.ISRC), track)
		claim(ctx, owned, domain.OwnershipKey(track.Title, track.Artist), track)
	}
	return owned, nil
}

func claim(ctx context.Context, owned map[string]ports.OwnedTrack, key string, track ports.OwnedTrack) {
	if key == "" {
		return
	}
	if kept, taken := owned[key]; taken {
		slog.DebugContext(ctx, "ownership.duplicate_key",
			"kept_track_id", kept.TrackID, "dropped_track_id", track.TrackID)
		return
	}
	owned[key] = track
}
