package ports

import (
	"context"

	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
)

type TrackRepository interface {
	// Add inserts the track, or returns the existing one on a dedup-key conflict.
	// The caller gets the persisted/existing track back (created reports which) —
	// no second lookup needed at the call site.
	Add(ctx context.Context, track *domain.Track) (stored *domain.Track, created bool, err error)
	// GetByID does not load FeaturedArtists — the lean read for callers that only
	// need status/audio-ref fields.
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
	GetByDedupKey(ctx context.Context, userId shared.UserId, dedupKey string) (*domain.Track, error)
	ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) (tracks []*domain.Track, total int, err error)
	// ListByIDs returns the user's tracks matching the given ids in one query —
	// the batch read for hot paths that would otherwise loop GetByID (audio-URL
	// presigning resolves up to 200 tracks per request). Unknown or foreign ids
	// are simply absent from the result; order is not guaranteed. Like the
	// playlist-track joins, it does not load featured credits.
	ListByIDs(ctx context.Context, userId shared.UserId, ids []domain.TrackId) ([]*domain.Track, error)
	Update(ctx context.Context, track *domain.Track) error
	// SetTrackNumber fills a track's album position when it is currently unset.
	// Fill-only (WHERE track_number IS NULL): it never overwrites an existing
	// value, so it is idempotent and safe to call repeatedly. Reports whether a
	// row was actually updated. Used to persist positions the client derived from
	// the album tracklist for tracks saved before track_number was captured.
	SetTrackNumber(ctx context.Context, id domain.TrackId, userId shared.UserId, trackNumber int) (updated bool, err error)
	Delete(ctx context.Context, id domain.TrackId, userId shared.UserId) (deleted bool, err error)
	// ReplaceFeaturedArtists replaces the full featured-artist set of a track
	// (used by the backfill). Idempotent — re-running with the same set is a no-op.
	ReplaceFeaturedArtists(ctx context.Context, id domain.TrackId, userId shared.UserId, feats []domain.FeaturedArtist) error
	// ListTracksFeaturing returns the user's tracks that credit the given featured
	// artist, matched on its identity key (MBID, else Deezer id, else name).
	ListTracksFeaturing(ctx context.Context, userId shared.UserId, fa domain.FeaturedArtist) ([]*domain.Track, error)
}
