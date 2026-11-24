package domain

import shareddomain "altune/go-api/internal/shared/domain"

var (
	ErrTrackNotFound                 = shareddomain.NewError(shareddomain.CodeCatalogTrackNotFound, "track not found")
	ErrPlaylistNotFound              = shareddomain.NewError(shareddomain.CodeCatalogPlaylistNotFound, "playlist not found")
	ErrAudioNotAvailable             = shareddomain.NewError(shareddomain.CodeCatalogAudioNotAvailable, "audio not available")
	ErrAudioTemporarilyUnavailable   = shareddomain.NewError(shareddomain.CodeCatalogAudioTemporarilyDown, "audio temporarily unavailable")
	ErrCatalogTemporarilyUnavailable = shareddomain.NewError(shareddomain.CodeCatalogTemporarilyDown, "catalog temporarily unavailable")
	ErrAudioOrphaned                 = shareddomain.NewError(shareddomain.CodeCatalogAudioOrphaned, "track deleted but audio file orphaned")
)

var ErrTrackAlreadyInPlaylist = shareddomain.NewError(shareddomain.CodeCatalogTrackAlreadyInPlaylist, "track already in playlist")

var (
	ErrTrackIDsRequired = shareddomain.NewError(shareddomain.CodeCatalogTrackIDsRequired, "track_ids required")
	ErrBatchTooLarge    = shareddomain.NewError(shareddomain.CodeCatalogBatchTooLarge, "too many track ids")
	ErrInvalidTrackID   = shareddomain.NewError(shareddomain.CodeCatalogInvalidTrackID, "invalid track id")

	ErrFeaturedArtistKeyRequired = shareddomain.NewError(shareddomain.CodeCatalogFeaturedArtistKeyRequired, "one of name, mbid, or deezer_id is required")
	ErrInvalidDeezerID           = shareddomain.NewError(shareddomain.CodeCatalogInvalidDeezerID, "deezer_id must be a positive integer")
)

func NewValidationError(msg string) *shareddomain.CodedError {
	return shareddomain.NewError(shareddomain.CodeCatalogValidation, msg)
}
