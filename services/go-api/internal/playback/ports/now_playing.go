package ports

import (
	"context"

	"altune/go-api/internal/shared"
)

// NowPlayingTrack is the catalog metadata used to enrich a resumed queue's
// current track. ArtworkURL and DurationSeconds are nil when the catalog has
// no value for them.
type NowPlayingTrack struct {
	Id                string
	Title             string
	Artist            string
	ArtworkURL        *string
	DurationSeconds   *float64
	AcquisitionStatus string
}

// NowPlayingReader resolves a queue track id to its catalog metadata.
type NowPlayingReader interface {
	// Lookup returns the track trackId names in userId's library. A track that
	// is absent (not found, owned by another user, or a trackId that is not a
	// valid track id) is (nil, nil), never an error, so callers must nil-check
	// the track even when err is nil. A non-nil error means the lookup itself
	// failed (e.g. the catalog is unavailable or timed out) and the track's
	// presence is unknown.
	Lookup(ctx context.Context, userId shared.UserId, trackId string) (*NowPlayingTrack, error)
}
