package ports

import (
	"altune/go-api/internal/catalog/domain"
	"altune/go-api/internal/shared"
	"context"
	"errors"
)

var ErrTrackVersionConflict = errors.New("track version conflict")

type TrackAdder interface {
	Add(ctx context.Context, track *domain.Track) (stored *domain.Track, created bool, err error)
}

type TrackGetter interface {
	GetByID(ctx context.Context, id domain.TrackId, userId shared.UserId) (*domain.Track, error)
}

type TrackBatchGetter interface {
	ListByIDs(ctx context.Context, userId shared.UserId, ids []domain.TrackId) ([]*domain.Track, error)
}

type TrackCounter interface {
	CountForUser(ctx context.Context, userId shared.UserId, atMost int) (int, error)
}

type TrackLister interface {
	ListForUser(ctx context.Context, userId shared.UserId, limit, offset int) (tracks []*domain.Track, total int, err error)
}

type TrackUpdater interface {
	Update(ctx context.Context, track *domain.Track, expectedVersion int) error
}

type TrackNumberSetter interface {
	SetTrackNumber(ctx context.Context, id domain.TrackId, userId shared.UserId, trackNumber int) (updated bool, err error)
}

type TrackDeleter interface {
	Delete(ctx context.Context, id domain.TrackId, userId shared.UserId) (deleted bool, audioRef *string, err error)
}

type TrackAudioRefLookup interface {
	AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error)
}

type TrackAudioDeleter interface {
	TrackDeleter
	TrackAudioRefLookup
}

type TrackReadWriter interface {
	TrackGetter
	TrackUpdater
}

type TrackNumberFiller interface {
	TrackGetter
	TrackNumberSetter
}

type TrackAddUpdater interface {
	TrackAdder
	TrackCounter
	TrackUpdater
}

type TrackLookup interface {
	TrackGetter
	TrackBatchGetter
}

type LibraryLensRepository interface {
	ListFilteredForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) (tracks []*domain.Track, total int, err error)
	ListAlbumsForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) ([]domain.AlbumGroup, error)
	ListArtistsForUser(ctx context.Context, userId shared.UserId, query domain.LibraryQuery) ([]domain.ArtistGroup, error)
}

type FeaturedArtistRepository interface {
	ListTracksFeaturing(ctx context.Context, userId shared.UserId, fa domain.FeaturedArtist) ([]*domain.Track, error)
	ReplaceFeaturedArtists(ctx context.Context, id domain.TrackId, userId shared.UserId, feats []domain.FeaturedArtist) error
}
