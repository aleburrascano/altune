package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
)

// TrackRepository persists individual tracks and their core lifecycle.
type TrackRepository interface {
	Add(ctx context.Context, track *domain.Track) (stored *domain.Track, created bool, err error)
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
	ListByIDs(ctx context.Context, userId shared.UserId, ids []domain.TrackId) ([]*domain.Track, error)
	ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) (tracks []*domain.Track, total int, err error)
	Update(ctx context.Context, track *domain.Track) error
	SetTrackNumber(ctx context.Context, id domain.TrackId, userId shared.UserId, trackNumber int) (updated bool, err error)
	Delete(ctx context.Context, id domain.TrackId, userId shared.UserId) (deleted bool, audioRef *string, err error)
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
