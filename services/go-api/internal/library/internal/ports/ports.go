package ports

import (
	"altune/go-api/internal/library/internal/domain"
	"altune/go-api/internal/shared/events"
	"altune/go-api/internal/shared/ids"
	"context"
	"io"
	"strings"
	"time"
)

type TrackEvents interface {
	events.Publisher
	TrackAdded(ctx context.Context, userId ids.UserId, track *domain.Track)
}

type AcquisitionScheduler interface {
	Schedule(ctx context.Context, userId ids.UserId, trackId domain.TrackId, sourceURL string) error
}

func NoopAcquisitionScheduler() AcquisitionScheduler { return noopAcquisitionScheduler{} }

type noopAcquisitionScheduler struct{}

func (noopAcquisitionScheduler) Schedule(context.Context, ids.UserId, domain.TrackId, string) error {
	return nil
}

type AudioStream interface {
	io.ReadSeeker
	io.Closer
}

type AudioStore interface {
	Exists(ctx context.Context, audioRef string) (bool, error)
	Store(ctx context.Context, sourcePath string, audioRef string) error
	Stream(ctx context.Context, audioRef string) (AudioStream, int64, error)
	Delete(ctx context.Context, audioRef string) error
}

func AudioContentType(audioRef string) string {
	switch {
	case strings.HasSuffix(audioRef, ".m4a"):
		return "audio/mp4"
	case strings.HasSuffix(audioRef, ".opus"):
		return "audio/opus"
	case strings.HasSuffix(audioRef, ".ogg"):
		return "audio/ogg"
	default:
		return "audio/mpeg"
	}
}

const MaxPresignTTL = time.Hour

func ClampPresignTTL(ttl time.Duration) time.Duration {
	return min(ttl, MaxPresignTTL)
}

type AudioLinkSigner interface {
	SignsLinks() bool
	PresignGet(ctx context.Context, audioRef string, ttl time.Duration) (string, error)
}

type ObjectAge struct {
	AudioRef     string
	LastModified time.Time
}

type AudioAgeLister interface {
	ListWithAge(ctx context.Context, prefix string) ([]ObjectAge, error)
}

type AudioBackend struct {
	AudioStore
	Links AudioLinkSigner
	Ages  AudioAgeLister
}

type AudioStoreMetrics interface {
	PresignFailed()
	OrphanedDelete()
	StreamRecoveryTriggered()
	OrphanedAudioReconcileFailed()
}

type DBCallMetrics interface {
	DBCallTimedOut()
}

type FeaturedArtistResolver interface {
	Resolve(ctx context.Context, artist, title string) ([]domain.FeaturedArtist, error)
}

type FeaturedArtistRepository interface {
	ListTracksFeaturing(ctx context.Context, userId ids.UserId, fa domain.FeaturedArtist) ([]*domain.Track, error)
	ReplaceFeaturedArtists(ctx context.Context, id domain.TrackId, userId ids.UserId, feats []domain.FeaturedArtist) error
}

type FeaturedArtistCredit struct {
	Name     string
	MBID     string
	DeezerID int64
}

type FeaturedArtistCreditSource interface {
	FeaturedArtistCredits(ctx context.Context, artist, title string) ([]FeaturedArtistCredit, error)
}

type FSCallMetrics interface {
	FSCallRefused()
	FSCallAbandoned()
}

type OrphanedAudio struct {
	AudioRef   string
	UserId     ids.UserId
	TrackId    domain.TrackId
	RecordedAt time.Time
	NotBefore  time.Time
	Attempts   int
}

type OrphanedAudioRecorder interface {
	RecordOrphanedAudio(ctx context.Context, orphan OrphanedAudio) error
}

type AudioUsage int

const (
	AudioUnused AudioUsage = iota
	AudioReferenced
	AudioOwnerAcquiring
)

type OrphanedAudioQueue interface {
	OrphanedAudioRecorder
	ListOrphanedAudio(ctx context.Context, limit int) ([]OrphanedAudio, error)
	AudioUsage(ctx context.Context, audioRef string, owner ids.UserId) (AudioUsage, error)
	ResolveOrphanedAudio(ctx context.Context, audioRef string) error
	MarkOrphanedAudioAttempt(ctx context.Context, audioRef string, cause string) error
}

type PlaylistLifecycleRepository interface {
	Create(ctx context.Context, playlist *domain.Playlist) error
	CreateKeyed(ctx context.Context, playlist *domain.Playlist, idempotencyKey string) (stored *domain.Playlist, created bool, err error)
	GetByIdempotencyKey(ctx context.Context, userId ids.UserId, idempotencyKey string) (*domain.Playlist, bool, error)
	ListForUser(ctx context.Context, userId ids.UserId, limit, offset int) ([]domain.PlaylistWithSummary, error)
	CountForUser(ctx context.Context, userId ids.UserId, atMost int) (int, error)
	GetByID(ctx context.Context, id domain.PlaylistId, userId ids.UserId) (*domain.Playlist, domain.PlaylistSummary, error)
	GetWithTracks(ctx context.Context, id domain.PlaylistId, userId ids.UserId) (*domain.Playlist, []*domain.Track, error)
	Delete(ctx context.Context, id domain.PlaylistId, userId ids.UserId) (deleted bool, err error)
	Update(ctx context.Context, playlist *domain.Playlist) error
}

type PlaylistMembershipRepository interface {
	Exists(ctx context.Context, playlistId domain.PlaylistId, userId ids.UserId) (bool, error)
	GetTrackOrder(ctx context.Context, playlistId domain.PlaylistId, userId ids.UserId) (trackIds []domain.TrackId, found bool, err error)
	AddTrack(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) error
	AddTracks(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (added []domain.TrackId, err error)
	RemoveTrack(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId, trackId domain.TrackId) (removed bool, err error)
	RemoveTracks(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId, trackIds []domain.TrackId) (removed []domain.TrackId, err error)
	ReorderTracks(ctx context.Context, userId ids.UserId, playlistId domain.PlaylistId, tracks []domain.PlaylistTrack) error
}

type TrackAdder interface {
	Add(ctx context.Context, track *domain.Track) (stored *domain.Track, created bool, err error)
}

type TrackGetter interface {
	GetByID(ctx context.Context, id domain.TrackId, userId ids.UserId) (*domain.Track, error)
}

type TrackBatchGetter interface {
	ListByIDs(ctx context.Context, userId ids.UserId, ids []domain.TrackId) ([]*domain.Track, error)
}

type TrackCounter interface {
	CountForUser(ctx context.Context, userId ids.UserId, atMost int) (int, error)
}

type TrackLister interface {
	ListForUser(ctx context.Context, userId ids.UserId, limit, offset int) (tracks []*domain.Track, total int, err error)
}

type TrackUpdater interface {
	Update(ctx context.Context, track *domain.Track, expectedVersion int) error
}

type TrackNumberSetter interface {
	SetTrackNumber(ctx context.Context, id domain.TrackId, userId ids.UserId, trackNumber int) (updated bool, err error)
}

type TrackDeleter interface {
	Delete(ctx context.Context, id domain.TrackId, userId ids.UserId) (deleted bool, audioRef *string, err error)
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
	UserId   ids.UserId
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
	ListFilteredForUser(ctx context.Context, userId ids.UserId, query domain.LibraryQuery) (tracks []*domain.Track, total int, err error)
	ListAlbumsForUser(ctx context.Context, userId ids.UserId, query domain.LibraryQuery) ([]domain.AlbumGroup, error)
	ListArtistsForUser(ctx context.Context, userId ids.UserId, query domain.LibraryQuery) ([]domain.ArtistGroup, error)
}
