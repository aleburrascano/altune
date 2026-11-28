package domain

import "errors"

var (
	ErrTrackNotFound                 = errors.New("track not found")
	ErrPlaylistNotFound              = errors.New("playlist not found")
	ErrAudioNotAvailable             = errors.New("audio not available")
	ErrAudioTemporarilyUnavailable   = errors.New("audio temporarily unavailable")
	ErrCatalogTemporarilyUnavailable = errors.New("catalog temporarily unavailable")
	ErrAudioOrphaned                 = errors.New("track deleted but audio file orphaned")
	ErrStorageStalled                = errors.New("audio storage is stalled")
	ErrTrackAlreadyInPlaylist        = errors.New("track already in playlist")
	ErrPlaylistFull                  = errors.New("playlist is full")
	ErrLibraryFull                   = errors.New("library is full")
	ErrTooManyPlaylists              = errors.New("playlist limit reached")
	ErrBackfillInProgress            = errors.New("featured artist backfill already in progress")
	ErrBackfillCoolingDown           = errors.New("featured artist backfill ran recently; try again later")

	ErrTrackIDsRequired          = errors.New("track_ids required")
	ErrBatchTooLarge             = errors.New("too many track ids")
	ErrInvalidTrackID            = errors.New("invalid track id")
	ErrFeaturedArtistKeyRequired = errors.New("one of name, mbid, or deezer_id is required")
	ErrInvalidDeezerID           = errors.New("deezer_id must be a positive integer")

	ErrDBTransient                   = errors.New("catalog database temporarily unavailable")
	ErrOrphanedAudioQueueUnavailable = errors.New("orphaned audio queue unavailable")
	ErrPlaylistNotOwned              = errors.New("playlist not found for user")
	ErrTrackMissing                  = errors.New("track no longer exists")
	ErrTrackVersionConflict          = errors.New("track version conflict")
	ErrTrackFenceSuperseded          = errors.New("track claimed by a newer acquisition attempt")
	ErrIllegalAcquisitionTransition  = errors.New("illegal acquisition status transition")

	ErrTrackTimestampClockRequired = errors.New("domain: a track needs a clock")
	ErrIDSourceRequired            = errors.New("domain: an id source is required")

	ErrTrackClockRequired           = errors.New("track: the service requires a clock")
	ErrTrackIDSourceRequired        = errors.New("track: the service requires an id source")
	ErrTrackCollaboratorRequired    = errors.New("track: a required collaborator is missing")
	ErrPlaylistClockRequired        = errors.New("playlist: the service requires a clock")
	ErrPlaylistIDSourceRequired     = errors.New("playlist: the service requires an id source")
	ErrPlaylistCollaboratorRequired = errors.New("playlist: a required collaborator is missing")
	ErrAudioClockRequired           = errors.New("audio: the audio link service requires a clock")
	ErrBackfillClockRequired        = errors.New("featured: the backfill service requires a clock")
	ErrHandlerClockRequired         = errors.New("handler: the handler requires a clock")
	ErrDBCallMetricsRequired        = errors.New("persistence: the repository requires db call metrics")
	ErrAudioLinksUnsupported        = errors.New("the configured audio store cannot sign links")
	ErrAgeListingUnsupported        = errors.New("the configured audio store cannot list object age")
	ErrPlaylistChangedDuringReorder = NewValidationError("playlist changed during reorder")
)

type ValidationError struct{ msg string }

func NewValidationError(msg string) *ValidationError { return &ValidationError{msg: msg} }

func (e *ValidationError) Error() string { return e.msg }

func (e *ValidationError) Is(target error) bool {
	_, ok := target.(*ValidationError)
	return ok
}
