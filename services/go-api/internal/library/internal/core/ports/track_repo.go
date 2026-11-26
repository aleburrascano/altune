package ports

import (
	"altune/go-api/internal/library/internal/domain"
	sharedids "altune/go-api/internal/shared/ids"
	"context"
	"errors"
)

var (
	ErrTrackVersionConflict = errors.New("track version conflict")
	ErrTrackFenceSuperseded = errors.New("track claimed by a newer acquisition attempt")
)

type TrackAdder interface {
	Add(ctx context.Context, track *domain.Track) (stored *domain.Track, created bool, err error)
}

type TrackGetter interface {
	GetByID(ctx context.Context, id domain.TrackId, userId sharedids.UserId) (*domain.Track, error)
}

type TrackBatchGetter interface {
	ListByIDs(ctx context.Context, userId sharedids.UserId, ids []domain.TrackId) ([]*domain.Track, error)
}

type TrackCounter interface {
	CountForUser(ctx context.Context, userId sharedids.UserId, atMost int) (int, error)
}

type TrackLister interface {
	ListForUser(ctx context.Context, userId sharedids.UserId, limit, offset int) (tracks []*domain.Track, total int, err error)
}

type TrackUpdater interface {
	Update(ctx context.Context, track *domain.Track, expectedVersion int) error
}

type TrackNumberSetter interface {
	SetTrackNumber(ctx context.Context, id domain.TrackId, userId sharedids.UserId, trackNumber int) (updated bool, err error)
}

type TrackDeleter interface {
	Delete(ctx context.Context, id domain.TrackId, userId sharedids.UserId) (deleted bool, audioRef *string, err error)
}

type TrackAudioRefLookup interface {
	AudioRefInUse(ctx context.Context, audioRef string, excludeTrackID domain.TrackId) (bool, error)
}

type InPlaceVerdict struct {
	Verdict            string
	Score              float64
	Grade              string
	UserUploadMismatch bool
	MayLower           bool
	DropScore          bool
}

type ReadyAudioTrack struct {
	Id       domain.TrackId
	UserId   sharedids.UserId
	Title    string
	Artist   string
	AudioRef string
}

type ReadyAudioLister interface {
	ListReadyAudioTracks(ctx context.Context) ([]ReadyAudioTrack, error)
}

type AudioRefPrefixLister interface {
	AudioRefsUnderPrefix(ctx context.Context, prefix string) (map[string]bool, error)
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
	TrackUpdater
}

type TrackLookup interface {
	TrackGetter
	TrackBatchGetter
}

type LibraryViewRepository interface {
	ListFilteredForUser(ctx context.Context, userId sharedids.UserId, query domain.LibraryQuery) (tracks []*domain.Track, total int, err error)
	ListAlbumsForUser(ctx context.Context, userId sharedids.UserId, query domain.LibraryQuery) ([]domain.AlbumGroup, error)
	ListArtistsForUser(ctx context.Context, userId sharedids.UserId, query domain.LibraryQuery) ([]domain.ArtistGroup, error)
}
