package ports

import (
	"context"

	"altune/go-api/internal/shared"
)

// NowPlayingTrack is the minimal, display-ready snapshot of the currently-playing
// track that resume embeds in the queue-state response, so the client can render
// now-playing from the small queue-state call instead of waiting on a full library
// rehydrate. It mirrors catalog track data by value across the context seam; the
// bridge adapter (adapters/catalogbridge) produces it from the catalog Track.
type NowPlayingTrack struct {
	Id                string
	Title             string
	Artist            string
	ArtworkURL        *string
	DurationSeconds   *float64
	AcquisitionStatus string
}

// NowPlayingReader resolves one track id (owned by the catalog context) into a
// display snapshot. Consumed by the resume use case; implemented by the catalog
// bridge. The bool is false — with a nil error — when the track is absent
// (deleted, unknown, or a malformed id), which resume treats as "no snapshot".
type NowPlayingReader interface {
	Lookup(ctx context.Context, userId shared.UserId, trackId string) (*NowPlayingTrack, bool, error)
}
