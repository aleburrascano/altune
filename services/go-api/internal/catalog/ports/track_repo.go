package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
)

// The track lifecycle is served by one persistence adapter but consumed through
// narrow per-capability ports: each catalog service depends only on the methods
// it calls, so a test double or decorator implements just that slice.

// TrackAdder inserts a track, returning the stored row and whether it was newly
// created (false when an idempotency or dedup match already existed).
type TrackAdder interface {
	Add(ctx context.Context, track *domain.Track) (stored *domain.Track, created bool, err error)
}

// TrackGetter reads one owner-scoped track.
type TrackGetter interface {
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
}

// TrackBatchGetter reads the owner-scoped tracks among the given ids.
type TrackBatchGetter interface {
	ListByIDs(ctx context.Context, userId shared.UserId, ids []domain.TrackId) ([]*domain.Track, error)
}

// TrackLister pages through a user's tracks.
type TrackLister interface {
	ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) (tracks []*domain.Track, total int, err error)
}

// TrackUpdater persists changes to an existing track.
type TrackUpdater interface {
	Update(ctx context.Context, track *domain.Track) error
}

// TrackNumberSetter sets a track's album position.
type TrackNumberSetter interface {
	SetTrackNumber(ctx context.Context, id domain.TrackId, userId shared.UserId, trackNumber int) (updated bool, err error)
}

// TrackDeleter removes one owner-scoped track and returns its audio ref.
type TrackDeleter interface {
	Delete(ctx context.Context, id domain.TrackId, userId shared.UserId) (deleted bool, audioRef *string, err error)
}

// TrackReadWriter reads a track and writes it back.
type TrackReadWriter interface {
	TrackGetter
	TrackUpdater
}

// TrackAddUpdater inserts a track and writes back later changes to it.
type TrackAddUpdater interface {
	TrackAdder
	TrackUpdater
}

// TrackLookup reads tracks singly or in batches.
type TrackLookup interface {
	TrackGetter
	TrackBatchGetter
}

// LibraryLensRepository serves grouped, lens-style reads over a user's library.
type LibraryLensRepository interface {
	ListFilteredForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) (tracks []*domain.Track, total int, err error)
	ListAlbumsForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) ([]domain.AlbumGroup, error)
	ListArtistsForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) ([]domain.ArtistGroup, error)
}

// FeaturedArtistRepository manages featured-artist associations on tracks.
type FeaturedArtistRepository interface {
	ListTracksFeaturing(ctx context.Context, userId shared.UserId, fa domain.FeaturedArtist) ([]*domain.Track, error)
	ReplaceFeaturedArtists(ctx context.Context, id domain.TrackId, userId shared.UserId, feats []domain.FeaturedArtist) error
}
