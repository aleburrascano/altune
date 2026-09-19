package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
)

type AudioWriter interface {
	Exists(ctx context.Context, audioRef string) (bool, error)
	Store(ctx context.Context, sourcePath string, audioRef string) error
	Delete(ctx context.Context, audioRef string) error
}

// AudioRefLookup answers whether an audio object is still serving a track
// other than excludeTrackID. A canonical audioRef is derived from normalized
// metadata, so tracks with equivalent metadata share one object (migration
// 021): a compensating or superseding delete that skips this check can strip a
// Ready track of its file (#1984).
type AudioRefLookup interface {
	AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error)
}

type TrackRepository interface {
	AudioRefLookup
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
	// Update writes the track back under an optimistic-lock CAS at
	// expectedVersion (the domain.Track.Version read before mutating); see
	// catalog ports.TrackUpdater. A CAS miss yields catalog
	// ports.ErrTrackVersionConflict, distinct from a not-found/deleted error.
	Update(ctx context.Context, track *domain.Track, expectedVersion int) error
}
